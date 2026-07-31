package mcp

import "encoding/json"

// This file holds the protocol-era machinery for MCP dual-stack support.
// gridctl speaks two protocol generations concurrently, per peer:
//
//   - Handshake era (2025-11-25 and earlier): initialize handshake,
//     Mcp-Session-Id sessions, identity frozen at session creation.
//   - Stateless era (2026-07-28 and later): no handshake; every request
//     carries its protocol version, capabilities, and client identity in
//     _meta; server/discover advertises the server; MRTR replaces
//     server-initiated requests.
//
// Era is a property of the peer, never of an individual request, and it
// is normalized at the transport edges: everything inboard (router,
// access policy, call gates, pins, telemetry) stays era-free.

// StatelessProtocolVersion is the first stateless-era protocol revision.
const StatelessProtocolVersion = "2026-07-28"

// ProtocolEra classifies a protocol version by its lifecycle model.
type ProtocolEra string

const (
	// EraHandshake covers revisions that establish a session via the
	// initialize handshake (2025-11-25 and earlier).
	EraHandshake ProtocolEra = "handshake"
	// EraStateless covers revisions that carry per-request metadata
	// with no handshake (2026-07-28 and later).
	EraStateless ProtocolEra = "stateless"
)

// statelessProtocolVersions is the membership set for the stateless era.
// Like SupportedProtocolVersions, membership decides classification;
// version strings are never compared or parsed.
var statelessProtocolVersions = map[string]bool{
	StatelessProtocolVersion: true,
}

// EraOfVersion classifies a protocol version string into its era.
// Unknown versions classify as handshake: the handshake path is where
// version negotiation can counter-offer, so it is the safe default.
func EraOfVersion(v string) ProtocolEra {
	if statelessProtocolVersions[v] {
		return EraStateless
	}
	return EraHandshake
}

// _meta keys defined by the 2026-07-28 revision.
const (
	metaKeyProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaKeyClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaKeyClientInfo         = "io.modelcontextprotocol/clientInfo"
	metaKeyServerInfo         = "io.modelcontextprotocol/serverInfo"
)

// TasksExtensionID is the identifier of the official MCP tasks
// extension. gridctl proxies it opaquely between peers that both
// declare it; it never mediates task bookkeeping.
const TasksExtensionID = "io.modelcontextprotocol/tasks"

// Result types required on every stateless-era result.
const (
	ResultTypeComplete      = "complete"
	ResultTypeInputRequired = "input_required"
)

// Cache scopes for the CacheableResult fields.
const (
	CacheScopePublic  = "public"
	CacheScopePrivate = "private"
)

// MCP-reserved JSON-RPC error codes (2026-07-28 allocates -32020 to
// -32099 to the specification).
const (
	ErrCodeHeaderMismatch                  = -32020
	ErrCodeMissingRequiredClientCapability = -32021
	ErrCodeUnsupportedProtocolVersion      = -32022
)

// StatelessResultFields are the result-envelope fields the 2026-07-28
// revision requires on cacheable results. They are embedded in the
// shared result structs with omitempty tags: the handshake-era wire
// shape stays byte-identical, and the stateless transport edge fills
// them in. On the downstream leg the same fields capture what a modern
// server reported, feeding the gateway's cache-metadata aggregation.
type StatelessResultFields struct {
	ResultType string `json:"resultType,omitempty"`
	TTLMs      *int64 `json:"ttlMs,omitempty"`
	CacheScope string `json:"cacheScope,omitempty"`
}

// RequestMeta is the parsed modern per-request _meta envelope.
type RequestMeta struct {
	// ProtocolVersion is the declared version (required by the spec).
	ProtocolVersion string
	// ClientInfo identifies the caller (SHOULD per spec).
	ClientInfo    ClientInfo
	HasClientInfo bool
	// ClientCapabilities is the raw declared capability object
	// (required by the spec); byte-preserved, never interpreted
	// beyond presence.
	ClientCapabilities json.RawMessage
}

// parseRequestMeta extracts the modern _meta envelope from raw request
// params. The boolean reports whether modern _meta is present (the
// protocolVersion key): that presence is the stateless-era
// classification signal, so a request without it is handshake-era
// traffic regardless of the values of other keys.
func parseRequestMeta(params json.RawMessage) (RequestMeta, bool) {
	if len(params) == 0 {
		return RequestMeta{}, false
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil || envelope.Meta == nil {
		return RequestMeta{}, false
	}
	rawVersion, ok := envelope.Meta[metaKeyProtocolVersion]
	if !ok {
		return RequestMeta{}, false
	}
	var m RequestMeta
	// A non-string value leaves ProtocolVersion empty; the caller
	// rejects it as unsupported rather than treating it as legacy.
	_ = json.Unmarshal(rawVersion, &m.ProtocolVersion)
	if raw, ok := envelope.Meta[metaKeyClientInfo]; ok {
		if json.Unmarshal(raw, &m.ClientInfo) == nil {
			m.HasClientInfo = true
		}
	}
	if raw, ok := envelope.Meta[metaKeyClientCapabilities]; ok {
		m.ClientCapabilities = raw
	}
	return m, true
}

// DiscoverResult is the server/discover response (2026-07-28). Servers
// MUST implement server/discover; gridctl serves it for the gateway and
// sends it as the downstream era probe. ResultType, TTLMs, and
// CacheScope are always emitted (required fields, no omitempty).
type DiscoverResult struct {
	ResultType        string         `json:"resultType"`
	SupportedVersions []string       `json:"supportedVersions"`
	Capabilities      Capabilities   `json:"capabilities"`
	Instructions      string         `json:"instructions,omitempty"`
	TTLMs             int64          `json:"ttlMs"`
	CacheScope        string         `json:"cacheScope"`
	Meta              map[string]any `json:"_meta,omitempty"`
}

// UnsupportedVersionErrorData is the data payload of an
// UnsupportedProtocolVersionError (-32022).
type UnsupportedVersionErrorData struct {
	Supported []string `json:"supported"`
	Requested string   `json:"requested"`
}
