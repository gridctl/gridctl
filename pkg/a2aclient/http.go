package a2aclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	maxCardBytes  = 1 << 20
	maxRPCBytes   = 10 << 20
	sessionHeader = "X-Amzn-Bedrock-AgentCore-Runtime-Session-Id"
)

// Error contains only locally authored diagnostics, never downstream text.
type Error struct {
	Category  string
	Status    int
	Code      int
	Retryable bool
	cause     error
}

func (e *Error) Error() string {
	return fmt.Sprintf("a2a: %s (HTTP %d, RPC %d, retryable=%t)", e.Category, e.Status, e.Code, e.Retryable)
}

// Unwrap preserves cancellation identity without retaining transport errors.
func (e *Error) Unwrap() error { return e.cause }

func safeError(ctx context.Context, category string) error {
	return &Error{Category: category, cause: ctx.Err()}
}

func transportError(ctx context.Context, category string, err error) error {
	cause := ctx.Err()
	var timeout net.Error
	if cause == nil && (errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()) {
		cause = context.DeadlineExceeded
	}
	return &Error{Category: category, cause: cause}
}

// Options identifies operator-authorized destinations and discovery credentials.
type Options struct {
	Card     string
	Endpoint string
	Token    string
	Bedrock  bool
	Timeout  time.Duration
}

// Client owns a single bounded HTTP transport and a private discovery session.
// It performs no automatic RPC retries and has no ambient cookie credentials.
type Client struct {
	card             *url.URL
	endpoint         *url.URL
	token            string
	bedrock          bool
	discoverySession string
	http             *http.Client
}

// Close releases idle transport connections. In-flight calls are canceled by
// their owning adapter generation before transport cleanup.
func (c *Client) Close() {
	c.http.CloseIdleConnections()
}

// New constructs the transport. HTTP fixtures always dial verified loopback IPs.
func New(opts Options) (*Client, error) {
	card, err := ParseURL(opts.Card)
	if err != nil {
		return nil, err
	}
	var endpoint *url.URL
	if opts.Endpoint != "" {
		endpoint, err = ParseURL(opts.Endpoint)
		if err != nil {
			return nil, err
		}
	}
	if strings.ContainsAny(opts.Token, "\r\n") || opts.Timeout < 0 {
		return nil, errors.New("a2a: invalid transport options")
	}
	var session [16]byte
	if _, err := rand.Read(session[:]); err != nil {
		return nil, errors.New("a2a: session entropy unavailable")
	}
	session[6] = session[6]&0x0f | 0x40
	session[8] = session[8]&0x3f | 0x80
	s := hex.EncodeToString(session[:])
	c := &Client{card: card, endpoint: endpoint, token: opts.Token, bedrock: opts.Bedrock,
		discoverySession: s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// No environment proxy: in particular, fixture HTTP cannot leave loopback.
	transport.Proxy = nil
	transport.DialContext = dialDestination
	c.http = &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	return c, nil
}

func dialDestination(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("a2a: invalid dial destination")
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if !fixtureHost(host) {
		return dialer.DialContext(ctx, network, address)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errors.New("a2a: loopback resolution failed")
	}
	for _, ip := range ips {
		if !ip.IP.IsLoopback() {
			return nil, errors.New("a2a: non-loopback fixture destination")
		}
	}
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.New("a2a: loopback connection failed")
}

// CardResponse is bounded discovery evidence. Callers must keep it out of diagnostics.
type CardResponse struct {
	Body   []byte
	Header http.Header
}

// FetchCard performs a full GET, following at most three same-origin redirects.
func (c *Client) FetchCard(ctx context.Context) (CardResponse, error) {
	u := c.card
	for redirects := 0; ; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return CardResponse{}, safeError(ctx, "invalid_card_request")
		}
		c.credentials(req, c.discoverySession)
		resp, err := c.http.Do(req)
		if err != nil {
			return CardResponse{}, transportError(ctx, "card_transport_failed", err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			_ = resp.Body.Close()
			location, err := resp.Location()
			if err != nil || redirects >= 3 {
				return CardResponse{}, safeError(ctx, "card_redirect_refused")
			}
			next, err := ParseURL(location.String())
			if err != nil || !sameOrigin(c.card, next) {
				return CardResponse{}, safeError(ctx, "cross_origin_card_redirect")
			}
			u = next
			continue
		}
		body, err := readBounded(ctx, resp, maxCardBytes)
		if err != nil {
			return CardResponse{}, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			failure := &Error{Category: "card_http_failed", Status: resp.StatusCode, Retryable: resp.StatusCode == 429}
			var envelope struct {
				JSONRPC string          `json:"jsonrpc"`
				Error   json.RawMessage `json:"error"`
			}
			if decodeJSON(body, &envelope) == nil && envelope.JSONRPC == "2.0" {
				var remote *Error
				if errors.As(decodeRPCError(resp.StatusCode, envelope.Error), &remote) && remote.Category == "rpc_failed" {
					failure.Code = remote.Code
					failure.Retryable = failure.Retryable || remote.Retryable
				}
			}
			return CardResponse{Header: resp.Header.Clone()}, failure
		}
		return CardResponse{Body: body, Header: resp.Header.Clone()}, nil
	}
}

// ResolveEndpoint authorizes an advertised URL, or the explicit operator override.
func (c *Client) ResolveEndpoint(advertised string) (string, error) {
	u, err := ParseURL(advertised)
	if err != nil {
		return "", err
	}
	if c.endpoint != nil {
		return c.endpoint.String(), nil
	}
	if !sameOrigin(c.card, u) {
		return "", errors.New("a2a: cross_origin_rpc_endpoint requires a2a.endpoint")
	}
	return u.String(), nil
}

// RPC sends one request to an authorized endpoint and returns bounded wire bytes.
// Both success and error bodies are returned for dialect-aware JSON-RPC decoding.
func (c *Client) RPC(ctx context.Context, endpoint, version, session string, body []byte) ([]byte, int, error) {
	u, err := ParseURL(endpoint)
	if err != nil {
		return nil, 0, err
	}
	if c.endpoint != nil && u.String() != c.endpoint.String() || c.endpoint == nil && !sameOrigin(c.card, u) {
		return nil, 0, errors.New("a2a: unauthorized_rpc_endpoint")
	}
	if version != "1.0" && version != "0.3" {
		return nil, 0, errors.New("a2a: invalid dialect")
	}
	if c.bedrock && (!validSession(session) || session == c.discoverySession) {
		return nil, 0, errors.New("a2a: invalid conversation session")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, safeError(ctx, "invalid_rpc_request")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("A2A-Version", version)
	c.credentials(req, session)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, transportError(ctx, "rpc_transport_failed", err)
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		_ = resp.Body.Close()
		return nil, resp.StatusCode, &Error{Category: "rpc_redirect_refused", Status: resp.StatusCode}
	}
	result, err := readBounded(ctx, resp, maxRPCBytes)
	return result, resp.StatusCode, err
}

func validSession(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	return err == nil
}

func (c *Client) credentials(req *http.Request, session string) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if c.bedrock {
		req.Header.Set(sessionHeader, session)
	}
}

func readBounded(ctx context.Context, resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, transportError(ctx, "response_read_failed", err)
	}
	if int64(len(body)) > limit {
		return nil, safeError(ctx, "response_too_large")
	}
	return body, nil
}
