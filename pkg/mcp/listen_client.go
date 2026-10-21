package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

const (
	defaultListenBackoff    = time.Second
	defaultListenBackoffMax = 30 * time.Second
	listenBackoffJitterFrac = 0.2
	listenDialTimeout       = 10 * time.Second
)

const (
	subscriptionStreamOpen        = "open"
	subscriptionStreamClosed      = "closed"
	subscriptionStreamUnsupported = "unsupported"
)

// listenCancelSlot identifies one listen stream's cancel func so a later
// stream can clear its own slot without comparing functions.
type listenCancelSlot struct {
	cancel context.CancelFunc
}

// listenStream is one downstream subscriptions/listen response body.
type listenStream struct {
	body   io.ReadCloser
	cancel context.CancelFunc
	slot   *listenCancelSlot
	reqID  json.RawMessage
}

func (s *listenStream) Close() error {
	if s == nil {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}

// listenLoop is the gateway's downstream subscriptions/listen reader for
// one logical server. External URL servers have a single replica, so the
// loop holds that one HTTP client.
type listenLoop struct {
	name   string
	gen    uint64
	cancel context.CancelFunc

	mu    sync.Mutex
	state string
}

func (l *listenLoop) setState(state string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.state = state
	l.mu.Unlock()
}

func (l *listenLoop) getState() string {
	if l == nil {
		return subscriptionStreamClosed
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.state == "" {
		return subscriptionStreamClosed
	}
	return l.state
}

func (c *Client) listenHTTPClient() *http.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.listenHTTP == nil {
		c.listenHTTP = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   listenDialTimeout,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout: listenDialTimeout,
				ForceAttemptHTTP2:   true,
			},
		}
	}
	return c.listenHTTP
}

func (c *Client) trackListenCancel(cancel context.CancelFunc) *listenCancelSlot {
	slot := &listenCancelSlot{cancel: cancel}
	c.mu.Lock()
	prev := c.listenSlot
	c.listenSlot = slot
	c.mu.Unlock()
	if prev != nil && prev.cancel != nil {
		prev.cancel()
	}
	return slot
}

func (c *Client) cancelActiveListen() {
	c.mu.Lock()
	slot := c.listenSlot
	c.listenSlot = nil
	c.mu.Unlock()
	if slot != nil && slot.cancel != nil {
		slot.cancel()
	}
}

func (c *Client) clearListenCancel(slot *listenCancelSlot) {
	if slot == nil {
		return
	}
	c.mu.Lock()
	if c.listenSlot == slot {
		c.listenSlot = nil
	}
	c.mu.Unlock()
}

// openListen POSTs subscriptions/listen and returns the response stream.
// The request is built with newPOSTRequest so auth, tracing, _meta, and
// Mcp-Method stay in one place, then sent on a timeout-free client.
func (c *Client) openListen(ctx context.Context, kinds listenKinds) (*listenStream, error) {
	if c == nil {
		return nil, errors.New("no http client")
	}
	id := c.requestID.Add(1)
	idBytes, err := json.Marshal(id)
	if err != nil {
		return nil, fmt.Errorf("marshaling listen id: %w", err)
	}
	rawID := json.RawMessage(idBytes)
	notes, err := json.Marshal(kinds.wire())
	if err != nil {
		return nil, fmt.Errorf("marshaling listen filter: %w", err)
	}
	params := json.RawMessage(`{"notifications":` + string(notes) + `}`)
	if c.Era() == EraStateless {
		params = stampStatelessMeta(ctx, params, c.ProtocolVersion(), c.copyProtocolExtensions())
	}
	req := jsonrpc.Request{
		JSONRPC: "2.0",
		ID:      &rawID,
		Method:  MethodSubscriptionsListen,
		Params:  params,
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling listen request: %w", err)
	}
	httpReq, err := c.newPOSTRequest(ctx, body, MethodSubscriptionsListen, params)
	if err != nil {
		return nil, err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	httpReq = httpReq.WithContext(streamCtx)
	slot := c.trackListenCancel(cancel)
	resp, err := c.listenHTTPClient().Do(httpReq)
	if err != nil {
		c.clearListenCancel(slot)
		cancel()
		return nil, fmt.Errorf("sending listen request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		c.clearListenCancel(slot)
		cancel()
		return nil, &HTTPStatusError{Status: resp.StatusCode, Body: string(raw)}
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		resp.Body.Close()
		c.clearListenCancel(slot)
		cancel()
		return nil, fmt.Errorf("listen response is not an event stream")
	}
	return &listenStream{body: resp.Body, cancel: cancel, slot: slot, reqID: rawID}, nil
}

// readListenEvents reads the listen stream. The first event must be the
// acknowledgement. Later notifications are dispatched only when their
// subscription id matches the request id and allow reports the registration
// is still current. A false allow, or a cancelled context, stops the read
// without calling handlers.
func (c *Client) readListenEvents(ctx context.Context, stream *listenStream, allow func() bool) error {
	if stream == nil || stream.body == nil {
		return errors.New("no listen stream")
	}
	reader := bufio.NewReaderSize(stream.body, 64*1024)
	var dataLines []string
	sawAck := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if allow != nil && !allow() {
			return errStaleRegistration
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimRight(line, "\r\n")
			if len(line) == 0 {
				if len(dataLines) > 0 {
					payload := strings.Join(dataLines, "\n")
					dataLines = nil
					if payload != "" {
						if rerr := c.handleListenPayload(payload, stream.reqID, &sawAck, allow); rerr != nil {
							return rerr
						}
					}
				}
			} else if value, ok := sseDataField(line); ok {
				dataLines = append(dataLines, value)
			}
		}
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if errors.Is(err, io.EOF) && len(dataLines) > 0 {
				payload := strings.Join(dataLines, "\n")
				if rerr := c.handleListenPayload(payload, stream.reqID, &sawAck, allow); rerr != nil {
					return rerr
				}
			}
			if !sawAck {
				return errors.New("listen stream closed before acknowledgement")
			}
			return err
		}
	}
}

func (c *Client) handleListenPayload(payload string, reqID json.RawMessage, sawAck *bool, allow func() bool) error {
	message, err := classifyPeerMessage([]byte(payload))
	if err != nil {
		if c.logger != nil {
			c.logger.Debug("listen event skipped", "reason", "malformed")
		}
		return nil
	}
	if message.kind == stdioResponse {
		if !*sawAck {
			return errors.New("listen stream missing acknowledgement")
		}
		return io.EOF
	}
	if message.kind == stdioRequest {
		if c.logger != nil {
			c.logger.Debug("server request skipped", "method", message.method, "reason", "listen stream")
		}
		return nil
	}
	if !*sawAck {
		if message.method != NotificationSubscriptionsAcknowledged {
			return fmt.Errorf("listen stream first event was %s, want %s", message.method, NotificationSubscriptionsAcknowledged)
		}
		if !listenSubscriptionMatches(payload, reqID) {
			return errors.New("listen acknowledgement subscription id mismatch")
		}
		*sawAck = true
		return nil
	}
	if !listenSubscriptionMatches(payload, reqID) {
		if c.logger != nil {
			c.logger.Debug("server notification skipped", "method", message.method, "reason", "subscription id")
		}
		return nil
	}
	if allow != nil && !allow() {
		return errStaleRegistration
	}
	switch message.method {
	case MethodToolsListChanged:
		c.notifyListChanged()
	case MethodPromptsListChanged:
		c.notifyPromptsChanged()
	case MethodResourcesListChanged:
		c.notifyResourcesChanged()
	default:
		if c.logger != nil {
			c.logger.Debug("server notification skipped", "method", message.method, "reason", "listen stream")
		}
	}
	return nil
}

func listenSubscriptionMatches(payload string, reqID json.RawMessage) bool {
	var envelope struct {
		Params struct {
			Meta map[string]json.RawMessage `json:"_meta"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
		return false
	}
	got := envelope.Params.Meta[metaKeySubscriptionID]
	if len(got) == 0 || len(reqID) == 0 {
		return false
	}
	return sameJSONValue(got, reqID)
}

func sameJSONValue(a, b json.RawMessage) bool {
	var ab, bb bytes.Buffer
	if json.Compact(&ab, a) != nil || json.Compact(&bb, b) != nil {
		return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
	}
	return bytes.Equal(ab.Bytes(), bb.Bytes())
}

func listenKindsFromCaps(caps Capabilities) listenKinds {
	var kinds listenKinds
	if caps.Tools != nil && caps.Tools.ListChanged {
		kinds.tools = true
	}
	if caps.Prompts != nil && caps.Prompts.ListChanged {
		kinds.prompts = true
	}
	if caps.Resources != nil && caps.Resources.ListChanged {
		kinds.resources = true
	}
	return kinds
}

func httpListenClient(client AgentClient) *Client {
	switch c := client.(type) {
	case *Client:
		return c
	case *executionClient:
		return httpListenClient(c.AgentClient)
	default:
		return nil
	}
}

func (g *Gateway) subscriptionStreamFor(name string, client AgentClient) string {
	httpClient := httpListenClient(client)
	if httpClient == nil || protocolGenerationOf(client) != string(EraStateless) {
		return ""
	}
	if !listenKindsFromCaps(httpClient.DownstreamCapabilities()).any() {
		return subscriptionStreamUnsupported
	}
	g.pendingMu.Lock()
	loop := g.listenLoops[name]
	g.pendingMu.Unlock()
	if loop == nil {
		return subscriptionStreamClosed
	}
	return loop.getState()
}

func (g *Gateway) cancelListenLoopLocked(name string) {
	if g.listenLoops == nil {
		return
	}
	if loop := g.listenLoops[name]; loop != nil {
		loop.cancel()
		delete(g.listenLoops, name)
	}
}

func (g *Gateway) cancelListenLoops() {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	for name, loop := range g.listenLoops {
		if loop != nil && loop.cancel != nil {
			loop.cancel()
		}
		delete(g.listenLoops, name)
	}
}

func (g *Gateway) clearListenLoop(loop *listenLoop) {
	if g == nil || loop == nil {
		return
	}
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	if current := g.listenLoops[loop.name]; current == loop {
		delete(g.listenLoops, loop.name)
	}
}

func (g *Gateway) listenBackoff() (time.Duration, time.Duration) {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	initial, max := g.listenInitial, g.listenMax
	if initial <= 0 {
		initial = defaultListenBackoff
	}
	if max <= 0 {
		max = defaultListenBackoffMax
	}
	if max < initial {
		max = initial
	}
	return initial, max
}

// setListenBackoff overrides the downstream reconnect delay. Non-positive
// values keep the current setting. Tests use a short initial delay.
func (g *Gateway) setListenBackoff(initial, max time.Duration) {
	if g == nil {
		return
	}
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()
	if initial > 0 {
		g.listenInitial = initial
	}
	if max > 0 {
		g.listenMax = max
	}
}

func listenJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	span := float64(base) * listenBackoffJitterFrac
	offset := (rand.Float64()*2 - 1) * span //nolint:gosec // retry jitter, not security-sensitive
	delay := base + time.Duration(offset)
	if delay < 0 {
		return 0
	}
	return delay
}

// startListenLoop opens a downstream subscriptions/listen stream after a
// registration commit. It does not start when pin verification failed,
// the client is not the HTTP transport, the era is not stateless, or the
// server declares none of the three list-changed capabilities.
func (g *Gateway) startListenLoop(name string, gen uint64) {
	if g == nil {
		return
	}
	set := g.router.GetReplicaSet(name)
	if set == nil || set.Client() == nil {
		return
	}
	client := httpListenClient(set.Client())
	if client == nil || protocolGenerationOf(client) != string(EraStateless) {
		return
	}
	kinds := listenKindsFromCaps(client.DownstreamCapabilities())
	if !kinds.any() {
		return
	}
	if !g.registrationGenIs(name, gen) {
		return
	}
	ctx, cancel := context.WithCancel(g.lifeCtx)
	loop := &listenLoop{name: name, gen: gen, cancel: cancel, state: subscriptionStreamClosed}
	g.pendingMu.Lock()
	if g.regGen[name] != gen {
		g.pendingMu.Unlock()
		cancel()
		return
	}
	if g.listenLoops == nil {
		g.listenLoops = make(map[string]*listenLoop)
	}
	if old := g.listenLoops[name]; old != nil && old.cancel != nil {
		old.cancel()
	}
	g.listenLoops[name] = loop
	g.pendingMu.Unlock()
	go loop.run(ctx, g, client, kinds)
}

func (l *listenLoop) run(ctx context.Context, g *Gateway, client *Client, kinds listenKinds) {
	defer g.clearListenLoop(l)
	defer l.setState(subscriptionStreamClosed)
	initial, max := g.listenBackoff()
	delay := initial
	var attempt int
	for {
		if ctx.Err() != nil || !g.registrationGenIs(l.name, l.gen) {
			return
		}
		if client.Era() != EraStateless {
			g.logger.Info("listen stream stopped", "server", l.name, "reason", "era")
			return
		}
		attempt++
		if attempt == 1 {
			g.logger.Info("listen stream opened", "server", l.name, "attempt", attempt)
		} else {
			g.logger.Info("listen stream reconnecting", "server", l.name, "attempt", attempt)
		}
		err := g.readDownstreamListen(ctx, l, client, kinds, attempt)
		if ctx.Err() != nil || !g.registrationGenIs(l.name, l.gen) {
			return
		}
		if client.Era() != EraStateless {
			g.logger.Info("listen stream stopped", "server", l.name, "reason", "era")
			return
		}
		l.setState(subscriptionStreamClosed)
		if attempt > 1 {
			g.logger.Warn("listen stream reconnect failed", "server", l.name, "attempt", attempt, "error", err)
		}
		timer := time.NewTimer(listenJitter(delay))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay *= 2
		if delay > max {
			delay = max
		}
	}
}

func (g *Gateway) readDownstreamListen(ctx context.Context, loop *listenLoop, client *Client, kinds listenKinds, attempt int) error {
	stream, err := client.openListen(ctx, kinds)
	if err != nil {
		return err
	}
	defer stream.Close()
	defer client.clearListenCancel(stream.slot)
	if ctx.Err() != nil || !g.registrationGenIs(loop.name, loop.gen) || client.Era() != EraStateless {
		return errStaleRegistration
	}
	loop.setState(subscriptionStreamOpen)
	// Events may have been missed while the stream was down. Tool delivery
	// is fingerprinted, so an unchanged list does not emit a second
	// upstream event. The first connect follows registration, which already
	// notified resources; a later connect reindexes so a gap is not silent.
	g.refreshServerToolsFromNotification(ctx, loop.name)
	if attempt > 1 && kinds.resources {
		g.reindexResources(loop.name)
	}
	if ctx.Err() != nil || !g.registrationGenIs(loop.name, loop.gen) {
		return errStaleRegistration
	}
	return client.readListenEvents(ctx, stream, func() bool {
		return g.registrationGenIs(loop.name, loop.gen) && client.Era() == EraStateless
	})
}

// reindexResources rebuilds one server's resource index and then notifies
// upstream subscribers. The initial registration path notifies itself and
// calls scheduleResourceIndex without this second trigger.
func (g *Gateway) reindexResources(name string) {
	go func() {
		g.indexServerResources(name)
		set := g.router.GetReplicaSet(name)
		if g.listChanges == nil || set == nil || set.Client() == nil || !declares(set.Client(), wantsResources) {
			return
		}
		g.listChanges.triggerResources(name)
	}()
}
