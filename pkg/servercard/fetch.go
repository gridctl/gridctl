package servercard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultFetchTimeout = 10 * time.Second

// Fetch failures the REST and CLI callers map onto typed codes.
var (
	ErrNoCard      = errors.New("no server card")
	ErrInvalidCard = errors.New("invalid server card")
	ErrOffOrigin   = errors.New("off-origin server card remote")
	ErrFetchFailed = errors.New("server card fetch failed")
	ErrTemplateURL = errors.New("server card URL template")
)

// FetchOptions bounds a card or catalog fetch. Zero values use the
// package defaults. Client, when set, is used as-is except that a nil
// CheckRedirect is replaced so redirects are refused.
type FetchOptions struct {
	Timeout  time.Duration
	MaxBytes int64
	Client   *http.Client
}

// Fetch retrieves one Server Card. It sends no credentials, follows no
// redirects, and refuses a body over the byte bound.
func Fetch(ctx context.Context, rawURL string, opts FetchOptions) (*ServerCard, error) {
	body, err := getBounded(ctx, rawURL, MediaTypeServerCard, opts)
	if err != nil {
		return nil, err
	}
	var card ServerCard
	if err := json.Unmarshal(body, &card); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCard, err)
	}
	if err := Validate(&card); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCard, err)
	}
	return &card, nil
}

// FetchCatalog retrieves an AI Catalog document with the same bounds as Fetch.
func FetchCatalog(ctx context.Context, rawURL string, opts FetchOptions) (*Catalog, error) {
	body, err := getBounded(ctx, rawURL, MediaTypeCatalog, opts)
	if err != nil {
		return nil, err
	}
	var cat Catalog
	if err := json.Unmarshal(body, &cat); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCard, err)
	}
	if cat.Entries == nil {
		cat.Entries = []CatalogEntry{}
	}
	return &cat, nil
}

func getBounded(ctx context.Context, rawURL, accept string, opts FetchOptions) ([]byte, error) {
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrFetchFailed)
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxBodyBytes
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFetchFailed, err)
	}
	req.Header.Set("Accept", accept)
	req.Header.Del("Authorization")

	client := opts.Client
	if client == nil {
		client = &http.Client{CheckRedirect: refuseRedirect}
	} else if client.CheckRedirect == nil {
		clone := *client
		clone.CheckRedirect = refuseRedirect
		client = &clone
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFetchFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrNoCard, rawURL)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: %s returned %d", ErrFetchFailed, rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFetchFailed, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("%w: body exceeds %d bytes", ErrFetchFailed, maxBytes)
	}
	return body, nil
}

func refuseRedirect(req *http.Request, via []*http.Request) error {
	return fmt.Errorf("%w: redirects refused", ErrFetchFailed)
}

// CardURLFor appends /server-card to a server URL unless it already ends there.
func CardURLFor(raw string) (string, error) {
	u, err := parseHTTP(raw)
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(u.Path, "/")
	if !strings.HasSuffix(path, ServerCardSuffix) {
		path += ServerCardSuffix
	}
	u.Path = path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// CatalogURLFor returns the origin's /.well-known/ai-catalog.json.
func CatalogURLFor(raw string) (string, error) {
	u, err := parseHTTP(raw)
	if err != nil {
		return "", err
	}
	u.Path = CatalogWellKnown
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// SameOrigin reports whether two URLs share scheme, host, and port.
// Default ports are normalized so https://host and https://host:443 match.
func SameOrigin(a, b string) bool {
	ua, errA := parseHTTP(a)
	ub, errB := parseHTTP(b)
	if errA != nil || errB != nil {
		return false
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) &&
		strings.EqualFold(ua.Hostname(), ub.Hostname()) &&
		normalizedPort(ua) == normalizedPort(ub)
}

func parseHTTP(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("%w: %q is not an absolute http(s) URL", ErrFetchFailed, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%w: %q is not an absolute http(s) URL", ErrFetchFailed, raw)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%w: %q must not carry userinfo", ErrFetchFailed, raw)
	}
	return u, nil
}

func normalizedPort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}
