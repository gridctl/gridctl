package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync/atomic"

	"github.com/gridctl/gridctl/pkg/a2aclient"
)

// CardSnapshotBuilder keeps pin hashing in the pin package without a dependency
// cycle. Implementations return immutable, complete, digest-only evidence.
type CardSnapshotBuilder interface {
	BuildCardSnapshot(uint64, string, string, string, string, []byte, []Tool) (PinSnapshot, error)
}

type a2aSnapshot struct {
	PinSnapshot
	endpoint, version string
	tools             a2aToolSet
}

type a2aResultBudgetKey struct{}

func (g *Gateway) buildA2AClient(ctx context.Context, cfg MCPServerConfig) (AgentClient, error) {
	if cfg.Execution != nil {
		return nil, terminalRegistration(a2aLocalError("execution_admission_unavailable"))
	}
	if cfg.A2AConfig == nil {
		return nil, terminalRegistration(a2aLocalError("configuration_required"))
	}
	if err := g.cardTrust.lock(ctx); err != nil {
		return nil, err
	}
	builder, _ := g.cardTrust.storage.(CardSnapshotBuilder)
	g.cardTrust.unlock()
	client, err := newA2AClient(ctx, cfg.Name, *cfg.A2AConfig, g.capabilities, g.cardTrust, builder, 0)
	if err != nil {
		return nil, terminalRegistration(err)
	}
	client.SetToolWhitelist(slices.Clone(cfg.Tools))
	client.toolsChanged = g.router.RefreshTools
	client.approvalCheck = func(snapshot PinSnapshot) error {
		if g.pinningEnabledForServer(cfg.Name) {
			drifts, err := g.schemaVerifier.VerifyOrPin(cfg.Name, snapshot.Records())
			if err != nil || len(drifts) != 0 {
				return a2aLocalError("tool_pin_verification_failed")
			}
		}
		// This map owns only legacy schema blocks. Card trust and policy gates
		// remain independent, and publication still holds the generation gate.
		g.UnblockServer(cfg.Name)
		return nil
	}
	if err := client.Initialize(ctx); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// A2AClient exposes the bounded outbound A2A subset. Its authority and trust are
// gateway-owned; neither caller labels nor remote metadata select a conversation.
type A2AClient struct {
	ClientBase
	name          string
	cfg           A2AClientConfig
	wire          *a2aclient.Client
	cache         *a2aclient.CardCache
	store         *CapabilityStore
	trust         *CardTrustService
	builder       CardSnapshotBuilder
	registration  uint64
	limit         int
	sequence      atomic.Uint64
	dialect       atomic.Value
	toolsChanged  func()
	approvalCheck func(PinSnapshot) error
	// Protected by trust.gate, including retirement callbacks. The lock order
	// is trust, then capability store; neither is held over discovery or RPC.
	authority *capabilityGeneration
	published *a2aSnapshot
	closed    bool
}

func newA2AClient(ctx context.Context, name string, cfg A2AClientConfig, store *CapabilityStore, trust *CardTrustService, builder CardSnapshotBuilder, limit int) (*A2AClient, error) {
	if store == nil || trust == nil || builder == nil || name == "" {
		return nil, a2aLocalError("card_pin_storage_unavailable")
	}
	wire, err := a2aclient.New(a2aclient.Options{Card: cfg.Card, Endpoint: cfg.Endpoint, Token: cfg.Token, Bedrock: cfg.Profile == "bedrock", Timeout: cfg.Timeout})
	if err != nil {
		return nil, err
	}
	if err := trust.lock(ctx); err != nil {
		wire.Close()
		return nil, err
	}
	defer trust.unlock()
	if trust.closed {
		wire.Close()
		return nil, a2aLocalError("card_trust_closed")
	}
	// Include explicit registrations used by other snapshot sources.
	for _, entry := range trust.entries {
		trust.nextGeneration = max(trust.nextGeneration, entry.generation)
	}
	trust.nextGeneration++
	cfg.Include = slices.Clone(cfg.Include)
	return &A2AClient{name: name, cfg: cfg, wire: wire, cache: a2aclient.NewCardCache(wire, cfg.Dialect), store: store, trust: trust, builder: builder, registration: trust.nextGeneration, limit: limit}, nil
}

// Name returns the configured logical server name.
func (c *A2AClient) Name() string { return c.name }

func (c *A2AClient) fetchSnapshot(ctx context.Context, force bool) (*a2aSnapshot, error) {
	body, err := c.cache.Fetch(ctx, force)
	if err != nil {
		return nil, err
	}
	card, iface, err := a2aclient.ParseCard(body, c.cfg.Dialect)
	if err != nil {
		return nil, err
	}
	endpoint, err := c.wire.ResolveEndpoint(iface.URL)
	if err != nil {
		return nil, err
	}
	version := "0.3"
	if len(iface.ProtocolVersion) >= 3 && iface.ProtocolVersion[:3] == "1.0" {
		version = "1.0"
	}
	tools, err := buildA2ATools(c.name, c.cfg, card, version)
	if err != nil {
		return nil, err
	}
	snapshot, err := c.builder.BuildCardSnapshot(c.registration, c.cfg.Card, c.cfg.Endpoint, c.cfg.Dialect, c.cfg.Profile, body, tools.tools)
	if err != nil {
		return nil, a2aLocalError("invalid_pin_snapshot")
	}
	return &a2aSnapshot{PinSnapshot: snapshot, endpoint: endpoint, version: version, tools: tools}, nil
}

// Initialize persists first-use trust before publishing any tools or authority.
func (c *A2AClient) Initialize(ctx context.Context) error {
	if err := c.trust.lock(ctx); err != nil {
		return err
	}
	closed := c.closed || c.trust.closed
	c.trust.unlock()
	if closed {
		return a2aLocalError("client_closed")
	}
	snapshot, err := c.fetchSnapshot(ctx, false)
	if err != nil {
		return err
	}
	_, err = c.trust.register(ctx, c.name, snapshot, func(ctx context.Context) (PinSnapshot, error) {
		return c.fetchSnapshot(ctx, true)
	}, c.retireLocked, c.publishLocked, c.approvalCheck)
	if err != nil {
		return err
	}
	// A pending card has no callable tools until approval publishes it. Keeping
	// the live registration lets hash-bound approval activate that exact instance.
	c.SetInitialized(ServerInfo{Name: c.name})
	return nil
}

func (c *A2AClient) retireLocked() {
	if c.authority != nil {
		c.authority.retire()
		c.authority = nil
	}
}

// withApproved binds admission/publication to the same gate as drift retirement
// and approval. The callback must perform only short local work.
func (c *A2AClient) withApproved(ctx context.Context, fn func(*a2aSnapshot) error) error {
	if err := c.trust.lock(ctx); err != nil {
		return err
	}
	defer c.trust.unlock()
	e := c.trust.entries[c.name]
	if c.closed || e == nil || e.generation != c.registration || e.blocked || e.approved == nil {
		return a2aLocalError("card_approval_required")
	}
	snapshot, ok := e.approved.(*a2aSnapshot)
	if !ok {
		return a2aLocalError("invalid_pin_snapshot")
	}
	if c.authority == nil || c.published == nil {
		return a2aLocalError("card_approval_required")
	}
	return fn(snapshot)
}

func (c *A2AClient) publishLocked(evidence PinSnapshot) error {
	snapshot, ok := evidence.(*a2aSnapshot)
	if !ok || snapshot.Generation() != c.registration || c.closed {
		return a2aLocalError("invalid_pin_snapshot")
	}
	if c.authority == nil {
		card, err := a2aclient.ParseURL(c.cfg.Card)
		if err != nil {
			return err
		}
		c.authority, err = c.store.boundGeneration(capabilityIdentity{server: c.name, card: card.String(), endpoint: snapshot.endpoint, dialect: snapshot.version, profile: c.cfg.Profile, cardDigest: snapshotRecord(snapshot, "_agent_card")})
		if err != nil {
			return err
		}
	}
	if c.published != snapshot {
		var tools []Tool
		for _, tool := range snapshot.Records() {
			if tool.Name != "_agent_card" && tool.Name != "_agent_identity" {
				tools = append(tools, tool)
			}
		}
		c.SetTools(tools)
		c.published = snapshot
		c.dialect.Store(snapshot.version)
		if c.toolsChanged != nil {
			c.toolsChanged()
		}
	}
	return nil
}

// PinSnapshot returns the trust service's current immutable review evidence.
// The existing snapshot interface has no context; this performs no network I/O.
func (c *A2AClient) PinSnapshot() PinSnapshot {
	snapshot, _ := c.trust.Snapshot(context.Background(), c.name)
	if snapshot != nil && snapshot.Generation() == c.registration {
		return snapshot
	}
	return nil
}

// RefreshTools checks card bytes without making candidate tools callable.
func (c *A2AClient) RefreshTools(ctx context.Context) error {
	snapshot, err := c.fetchSnapshot(ctx, false)
	if err != nil {
		return err
	}
	if err := c.trust.Observe(ctx, c.name, snapshot); err != nil {
		return err
	}
	return c.withApproved(ctx, func(*a2aSnapshot) error { return nil })
}

// Ping reports local trust health. It does not poll remote work or refresh cards
// ahead of capability admission; idle drift is detected by the next valid call.
func (c *A2AClient) Ping(ctx context.Context) error {
	return c.withApproved(ctx, func(*a2aSnapshot) error { return nil })
}

// Close retires local authority and cancels admitted HTTP calls. It never sends
// remote CancelTask and cannot prove that remote work has stopped.
func (c *A2AClient) Close() error {
	ctx := context.Background()
	if err := c.trust.lock(ctx); err != nil {
		return err
	}
	defer c.trust.unlock()
	c.closed = true
	c.retireLocked()
	c.wire.Close()
	if e := c.trust.entries[c.name]; e != nil && e.generation == c.registration && e.refetch != nil {
		e.unregister()
	}
	return nil
}

// CallTool performs capability admission before discovery, then rechecks trust
// and the operation revision at dispatch and result commit.
func (c *A2AClient) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	markSensitiveExecution(ctx)
	result, err := c.call(ctx, name, arguments)
	if err == nil {
		return result, nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, safeCallError(err)
	}
	version, _ := c.dialect.Load().(string)
	return a2aErrorResult(err, version), nil
}

func (c *A2AClient) call(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	limit := c.limit
	if configured, ok := ctx.Value(a2aResultBudgetKey{}).(int); ok {
		limit = configured
	}
	if err := checkA2AResultBudget(limit); err != nil {
		return nil, err
	}
	var args a2aArguments
	var op *capabilityOperation
	var admitted *a2aSnapshot
	err := c.withApproved(ctx, func(snapshot *a2aSnapshot) error {
		// Routing accepts canonical names independently of catalog visibility.
		// Enforce this source's configured callable whitelist at the adapter too.
		if !slices.ContainsFunc(c.Tools(), func(tool Tool) bool { return tool.Name == name }) {
			return a2aLocalError("unknown_tool")
		}
		var err error
		args, err = snapshot.tools.arguments(name, arguments)
		if err != nil {
			return err
		}
		op, err = c.authority.admit(ctx, args.operation, args.contextHandle, args.taskHandle)
		admitted = snapshot
		return err
	})
	if err != nil {
		return nil, err
	}
	// Pre-dispatch failures release reservations. Ambiguous outcomes below mark
	// uncertainty explicitly; fail is idempotent after commit or explicit failure.
	defer op.fail(false)
	fresh, err := c.fetchSnapshot(op.ctx, false)
	if err != nil {
		return nil, err
	}
	if err := c.trust.Observe(op.ctx, c.name, fresh); err != nil {
		return nil, err
	}
	var routing capabilityRouting
	err = c.withApproved(op.ctx, func(snapshot *a2aSnapshot) error {
		if snapshot.Hash() != admitted.Hash() {
			return a2aLocalError("card_approval_required")
		}
		var err error
		routing, err = op.dispatch()
		return err
	})
	if err != nil {
		return nil, err
	}
	request := args.request
	request.ID = strconv.FormatUint(c.sequence.Add(1), 10)
	request.MessageID = request.ID
	request.ContextID, request.TaskID = routing.contextID, routing.taskID
	operation := a2aclient.Operation(args.operation) + a2aclient.Send
	body, err := a2aclient.EncodeRequest(admitted.version, operation, request)
	if err != nil {
		return nil, err
	}
	body, status, err := c.wire.RPC(op.ctx, admitted.endpoint, admitted.version, routing.session, body)
	if err != nil {
		op.fail(true)
		return nil, err
	}
	response, err := a2aclient.DecodeResponse(admitted.version, operation, request.ID, status, body)
	if err != nil {
		var remote *a2aclient.Error
		known := errors.As(err, &remote) && (remote.Category == "rpc_failed" || remote.Category == "http_failed" && (status == 401 || status == 403 || status == 409 || status == 424 || status == 429))
		op.fail(!known)
		return nil, err
	}
	envelope, err := prepareA2AEnvelope(response, limit)
	if err != nil {
		op.fail(true)
		return nil, err
	}
	var delivery capabilityDelivery
	err = c.withApproved(op.ctx, func(snapshot *a2aSnapshot) error {
		if snapshot.Hash() != admitted.Hash() {
			return a2aLocalError("card_approval_required")
		}
		var err error
		delivery, err = op.commit(a2aAdmissionResponse(response))
		return err
	})
	if err != nil {
		op.fail(true)
		return nil, err
	}
	return envelope.deliver(delivery)
}

func a2aErrorResult(err error, version string) *ToolCallResult {
	value := struct {
		Error     string `json:"error"`
		Dialect   string `json:"dialect,omitempty"`
		HTTP      int    `json:"http_status,omitempty"`
		RPC       int    `json:"rpc_code,omitempty"`
		Retryable bool   `json:"retryable"`
	}{Error: "a2a_call_failed", Dialect: version}
	var remote *a2aclient.Error
	var capability *capabilityError
	if errors.As(err, &remote) {
		value.Error, value.HTTP, value.RPC, value.Retryable = remote.Category, remote.Status, remote.Code, remote.Retryable
	} else if errors.As(err, &capability) {
		value.Error, value.Retryable = capability.code, capability.retryable
	} else if errors.Is(err, errCardApprovalRequired) {
		value.Error = "card_approval_required"
	}
	body, _ := json.Marshal(value)
	return &ToolCallResult{IsError: true, Content: []Content{NewTextContent(string(body))}, atomicResult: true}
}
