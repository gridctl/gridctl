package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

const (
	maxListenStreamsPerClient = 16
	maxListenStreams          = 256
	listenKeepalive           = 30 * time.Second
	listenWriteTimeout        = 2 * time.Second
)

// listenKinds is the honored subset of a subscriptions/listen filter.
// Resource update subscriptions are never set.
type listenKinds struct {
	tools, prompts, resources bool
}

func (k listenKinds) any() bool {
	return k.tools || k.prompts || k.resources
}

func (k listenKinds) wire() NotificationSubscriptions {
	return NotificationSubscriptions{
		ToolsListChanged:     k.tools,
		PromptsListChanged:   k.prompts,
		ResourcesListChanged: k.resources,
	}
}

// listenSubscriber is one open upstream subscriptions/listen stream.
// The notifier sets pending flags and wakes the writer; it never writes
// the response itself.
type listenSubscriber struct {
	key       uint64
	id        json.RawMessage
	accessID  string
	group     string
	principal bool
	// discovery is the override captured when the stream opened. Empty
	// means the stack default. Frozen for the stream's lifetime.
	discovery string
	kinds     listenKinds
	toolFP    string
	pending   listenKinds
	wake      chan struct{}
	cancel    context.CancelFunc
	graceful  atomic.Bool
}

// listenRegistry tracks open upstream listen streams and enforces the
// per-identity and overall caps. Its mutex is never held across a call
// that takes g.mu or the router lock.
type listenRegistry struct {
	mu       sync.Mutex
	next     uint64
	subs     map[uint64]*listenSubscriber
	byClient map[string]int
	closed   bool
}

func newListenRegistry() *listenRegistry {
	return &listenRegistry{
		subs:     make(map[uint64]*listenSubscriber),
		byClient: make(map[string]int),
	}
}

// listenLimitError is a cap rejection. The message names the limit so the
// JSON-RPC error tells the client which cap it hit.
type listenLimitError struct {
	limit int
	scope string
}

func (e *listenLimitError) Error() string {
	return fmt.Sprintf("listen stream limit exceeded: %d %s", e.limit, e.scope)
}

func (reg *listenRegistry) add(sub *listenSubscriber) error {
	if reg == nil {
		return fmt.Errorf("listen streams are closed")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.closed {
		return fmt.Errorf("listen streams are closed")
	}
	if reg.byClient[sub.accessID] >= maxListenStreamsPerClient {
		return &listenLimitError{limit: maxListenStreamsPerClient, scope: "per client"}
	}
	if len(reg.subs) >= maxListenStreams {
		return &listenLimitError{limit: maxListenStreams, scope: "overall"}
	}
	reg.next++
	sub.key = reg.next
	if sub.wake == nil {
		sub.wake = make(chan struct{}, 1)
	}
	reg.subs[sub.key] = sub
	reg.byClient[sub.accessID]++
	return nil
}

func (reg *listenRegistry) remove(key uint64) {
	if reg == nil {
		return
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	sub, ok := reg.subs[key]
	if !ok {
		return
	}
	delete(reg.subs, key)
	if n := reg.byClient[sub.accessID]; n <= 1 {
		delete(reg.byClient, sub.accessID)
	} else {
		reg.byClient[sub.accessID] = n - 1
	}
}

func (reg *listenRegistry) list() []*listenSubscriber {
	if reg == nil {
		return nil
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	out := make([]*listenSubscriber, 0, len(reg.subs))
	for _, sub := range reg.subs {
		out = append(out, sub)
	}
	return out
}

// CloseAll marks every stream graceful and cancels it so the writer can
// emit the final result before returning. A second call is a no-op for
// streams that have already been removed.
func (reg *listenRegistry) CloseAll() {
	if reg == nil {
		return
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	reg.closed = true
	for _, sub := range reg.subs {
		sub.graceful.Store(true)
		if sub.cancel != nil {
			sub.cancel()
		}
	}
}

func (reg *listenRegistry) wake(sub *listenSubscriber) {
	select {
	case sub.wake <- struct{}{}:
	default:
	}
}

func (reg *listenRegistry) noteTools(key uint64, fp string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	sub := reg.subs[key]
	if sub == nil || !sub.kinds.tools || sub.toolFP == fp {
		return false
	}
	sub.toolFP = fp
	sub.pending.tools = true
	reg.wake(sub)
	return true
}

func (reg *listenRegistry) toolsWouldChange(key uint64, fp string) bool {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	sub := reg.subs[key]
	return sub != nil && sub.kinds.tools && sub.toolFP != fp
}

func (reg *listenRegistry) noteLists(key uint64, prompts, resources bool) {
	if !prompts && !resources {
		return
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	sub := reg.subs[key]
	if sub == nil {
		return
	}
	changed := false
	if prompts && sub.kinds.prompts && !sub.pending.prompts {
		sub.pending.prompts = true
		changed = true
	}
	if resources && sub.kinds.resources && !sub.pending.resources {
		sub.pending.resources = true
		changed = true
	}
	if changed {
		reg.wake(sub)
	}
}

func (reg *listenRegistry) swapPending(key uint64) listenKinds {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	sub := reg.subs[key]
	if sub == nil {
		return listenKinds{}
	}
	pending := sub.pending
	sub.pending = listenKinds{}
	return pending
}

func honorListenKinds(caps Capabilities, want listenKinds) listenKinds {
	var got listenKinds
	if want.tools && caps.Tools != nil && caps.Tools.ListChanged {
		got.tools = true
	}
	if want.prompts && caps.Prompts != nil && caps.Prompts.ListChanged {
		got.prompts = true
	}
	if want.resources && caps.Resources != nil && caps.Resources.ListChanged {
		got.resources = true
	}
	return got
}

func parseListenNotifications(params json.RawMessage) (listenKinds, error) {
	if len(bytes.TrimSpace(params)) == 0 {
		return listenKinds{}, fmt.Errorf("params.notifications must be an object")
	}
	var envelope struct {
		Notifications json.RawMessage `json:"notifications"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return listenKinds{}, fmt.Errorf("params.notifications must be an object")
	}
	raw := bytes.TrimSpace(envelope.Notifications)
	if len(raw) == 0 || raw[0] != '{' {
		return listenKinds{}, fmt.Errorf("params.notifications must be an object")
	}
	var requested NotificationSubscriptions
	if err := json.Unmarshal(raw, &requested); err != nil {
		return listenKinds{}, fmt.Errorf("params.notifications must be an object")
	}
	return listenKinds{
		tools:     requested.ToolsListChanged,
		prompts:   requested.PromptsListChanged,
		resources: requested.ResourcesListChanged,
	}, nil
}

func copyRawID(id *json.RawMessage) json.RawMessage {
	if id == nil {
		return nil
	}
	return append(json.RawMessage(nil), (*id)...)
}

func subscriptionMetaRaw(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return json.RawMessage(`{"` + metaKeySubscriptionID + `":` + string(id) + `}`)
}

func listenAckPayload(id json.RawMessage, kinds listenKinds) []byte {
	notes, err := json.Marshal(kinds.wire())
	if err != nil {
		notes = []byte(`{}`)
	}
	params, err := json.Marshal(struct {
		Meta          json.RawMessage `json:"_meta"`
		Notifications json.RawMessage `json:"notifications"`
	}{Meta: subscriptionMetaRaw(id), Notifications: notes})
	if err != nil {
		return nil
	}
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}{JSONRPC: "2.0", Method: NotificationSubscriptionsAcknowledged, Params: params})
	if err != nil {
		return nil
	}
	return payload
}

func listenChangedPayload(method string, id json.RawMessage) []byte {
	params, err := json.Marshal(struct {
		Meta json.RawMessage `json:"_meta"`
	}{Meta: subscriptionMetaRaw(id)})
	if err != nil {
		return nil
	}
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}{JSONRPC: "2.0", Method: method, Params: params})
	if err != nil {
		return nil
	}
	return payload
}

func listenFinalPayload(id json.RawMessage) []byte {
	result, err := json.Marshal(struct {
		ResultType string          `json:"resultType"`
		Meta       json.RawMessage `json:"_meta"`
	}{ResultType: ResultTypeComplete, Meta: subscriptionMetaRaw(id)})
	if err != nil {
		return nil
	}
	rawID := id
	resp := jsonrpc.Response{JSONRPC: "2.0", ID: &rawID, Result: result}
	payload, err := json.Marshal(resp)
	if err != nil {
		return nil
	}
	return payload
}

func writeListenHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

func writeSSEMessage(w http.ResponseWriter, flusher http.Flusher, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("empty listen event")
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(listenWriteTimeout))
	defer func() { _ = rc.SetWriteDeadline(time.Time{}) }()
	if _, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeKeepalive(w http.ResponseWriter, flusher http.Flusher) error {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Now().Add(listenWriteTimeout))
	defer func() { _ = rc.SetWriteDeadline(time.Time{}) }()
	if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// handleStatelessListen serves subscriptions/listen. Validation of _meta
// and the header mirror has already run. A missing or non-object
// notifications filter is -32602 at HTTP 200, matching prompts/get.
func (s *StreamableHTTPServer) handleStatelessListen(ctx context.Context, w http.ResponseWriter, r *http.Request, req *jsonrpc.Request) {
	requested, err := parseListenNotifications(req.Params)
	if err != nil {
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InvalidParams, err.Error()))
		return
	}
	honored := honorListenKinds(s.gateway.advertisedCapabilities(EraStateless), requested)
	id := copyRawID(req.ID)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, "SSE not supported"))
		return
	}
	if !honored.any() {
		writeListenHeaders(w)
		flusher.Flush()
		_ = writeSSEMessage(w, flusher, listenAckPayload(id, honored))
		_ = writeSSEMessage(w, flusher, listenFinalPayload(id))
		return
	}

	accessID := ClientAccessIDFromContext(ctx)
	discovery := discoveryFromContext(ctx)
	sub := &listenSubscriber{
		id:        id,
		accessID:  accessID,
		group:     GroupFromContext(ctx),
		principal: PrincipalFromContext(ctx) != "",
		discovery: discovery,
		kinds:     honored,
		toolFP:    fingerprintTools(s.gateway.visibleToolsFor(accessID, GroupFromContext(ctx), PrincipalFromContext(ctx) != "", s.gateway.effectiveDiscovery(discovery))),
		wake:      make(chan struct{}, 1),
	}
	streamCtx, cancel := context.WithCancel(ctx)
	sub.cancel = cancel
	if err := s.gateway.listeners.add(sub); err != nil {
		cancel()
		if limit, ok := err.(*listenLimitError); ok {
			s.gateway.logger.Warn("listen stream rejected", "client", accessID, "limit", limit.limit)
		}
		writeStatelessResponse(w, http.StatusOK, jsonrpc.NewErrorResponse(req.ID, jsonrpc.InternalError, err.Error()))
		return
	}
	s.gateway.logger.Info("listen stream opened", "client", accessID, "subscription", string(id))
	s.serveListen(streamCtx, w, flusher, sub)
}

func (s *StreamableHTTPServer) serveListen(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, sub *listenSubscriber) {
	defer s.gateway.listeners.remove(sub.key)
	writeListenHeaders(w)
	flusher.Flush()
	if err := writeSSEMessage(w, flusher, listenAckPayload(sub.id, sub.kinds)); err != nil {
		return
	}
	ticker := time.NewTicker(listenKeepalive)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if sub.graceful.Load() {
				_ = writeSSEMessage(w, flusher, listenFinalPayload(sub.id))
			}
			return
		case <-sub.wake:
			if err := s.drainListen(ctx, w, flusher, sub); err != nil {
				return
			}
		case <-ticker.C:
			if err := writeKeepalive(w, flusher); err != nil {
				return
			}
		}
	}
}

func (s *StreamableHTTPServer) drainListen(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, sub *listenSubscriber) error {
	pending := s.gateway.listeners.swapPending(sub.key)
	if pending.tools {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeSSEMessage(w, flusher, listenChangedPayload(MethodToolsListChanged, sub.id)); err != nil {
			return err
		}
		s.gateway.logger.Debug("list_changed sent", "subscription", string(sub.id), "kind", "tools")
	}
	if pending.prompts {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeSSEMessage(w, flusher, listenChangedPayload(MethodPromptsListChanged, sub.id)); err != nil {
			return err
		}
		s.gateway.logger.Debug("list_changed sent", "subscription", string(sub.id), "kind", "prompts")
	}
	if pending.resources {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeSSEMessage(w, flusher, listenChangedPayload(MethodResourcesListChanged, sub.id)); err != nil {
			return err
		}
		s.gateway.logger.Debug("list_changed sent", "subscription", string(sub.id), "kind", "resources")
	}
	return nil
}
