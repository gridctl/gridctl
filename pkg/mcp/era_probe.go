package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Downstream era resolution: gridctl probes each MCP server with
// server/discover before anything else. A positively modern answer
// selects the stateless era; a recognized modern error identifies a
// modern server that needs different handling; everything else falls
// back to the legacy initialize handshake on the same connection, per
// the 2026-07-28 backward-compatibility rules.

// Generation pin values, matching the stack.yaml protocol_generation
// vocabulary.
const (
	GenerationAuto      = "auto"
	GenerationHandshake = "handshake"
	GenerationStateless = "stateless"
)

// discoverProbeTimeout bounds the era probe so a legacy server that
// silently swallows unknown methods cannot stall registration; the
// legacy fallback still runs within the caller's deadline.
const discoverProbeTimeout = 10 * time.Second

// RPCError is a JSON-RPC error returned by a downstream server, with
// the code and data preserved so callers can recognize spec-defined
// errors (the probe needs -32022's supported list).
type RPCError struct {
	Code    int
	Message string
	Data    json.RawMessage
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// HTTPStatusError is a non-200 downstream HTTP response. RPCErr carries
// the JSON-RPC error parsed from the body when one is present: modern
// servers put UnsupportedProtocolVersionError and friends inside 400s.
type HTTPStatusError struct {
	Status int
	Body   string
	RPCErr *RPCError
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// isRecognizedModernError reports whether err carries a JSON-RPC error
// code that only a stateless-era server emits.
func isRecognizedModernError(err error) bool {
	if rpcErr := rpcErrorFrom(err); rpcErr != nil {
		switch rpcErr.Code {
		case ErrCodeHeaderMismatch, ErrCodeMissingRequiredClientCapability, ErrCodeUnsupportedProtocolVersion:
			return true
		}
	}
	return false
}

// marshalErrorData renders a decoded JSON-RPC error data value back to
// raw bytes so typed errors can carry it without committing to a shape.
func marshalErrorData(data any) json.RawMessage {
	if data == nil {
		return nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return raw
}

// rpcErrorFrom extracts an RPCError from err, looking through
// HTTPStatusError bodies.
func rpcErrorFrom(err error) *RPCError {
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) {
		return httpErr.RPCErr
	}
	return nil
}

// statelessMetaMap builds the required per-request _meta for outbound
// stateless-era requests.
func statelessMetaMap(version string) map[string]any {
	return map[string]any{
		metaKeyProtocolVersion: version,
		metaKeyClientInfo: map[string]any{
			"name":    "gridctl-gateway",
			"version": "1.0.0",
		},
		metaKeyClientCapabilities: map[string]any{},
	}
}

// upstreamCapsKey carries the upstream client's declared capabilities
// from the stateless edge to the downstream stamping. Without this a
// compliant modern server would see the gateway's empty capability set
// and could never send input_required, making MRTR unreachable through
// the gateway. A handshake-era upstream leaves it unset, so modern
// servers correctly never start MRTR round trips that era cannot relay.
type upstreamCapsKey struct{}

func withUpstreamClientCapabilities(ctx context.Context, raw json.RawMessage) context.Context {
	if len(raw) == 0 {
		return ctx
	}
	return context.WithValue(ctx, upstreamCapsKey{}, raw)
}

func upstreamClientCapabilitiesFromContext(ctx context.Context) json.RawMessage {
	v, _ := ctx.Value(upstreamCapsKey{}).(json.RawMessage)
	return v
}

// noMutualVersionError names the failure shape shared by the two spots
// that can positively identify a modern server without a usable common
// version.
func noMutualVersionError(serverVersions any) error {
	return fmt.Errorf("server speaks the stateless generation but no mutually supported protocol version exists (server: %v, gridctl: %s); run 'gridctl doctor' for per-server generation details",
		serverVersions, supportedProtocolVersionList())
}

// probeDiscover sends server/discover and classifies the peer. It
// returns (true, nil) when the server is positively modern and fully
// set up for the stateless era, (false, nil) when the caller should
// fall back to the legacy handshake, and a non-nil error when neither
// is safe: auth challenges and probe 5xx reject outright (the TS SDK
// precedent; a broken or unauthenticated server must not be
// misclassified as legacy), as does a positively modern server with no
// mutually supported version.
func (r *RPCClient) probeDiscover(ctx context.Context) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, discoverProbeTimeout)
	defer cancel()

	params := map[string]any{"_meta": statelessMetaMap(StatelessProtocolVersion)}
	var result DiscoverResult
	err := r.transport.call(probeCtx, "server/discover", params, &result)
	if err == nil {
		return r.adoptDiscoverResult(result)
	}

	var authErr *AuthRequiredError
	if errors.As(err, &authErr) {
		return false, fmt.Errorf("server/discover probe: %w", err)
	}
	var httpErr *HTTPStatusError
	if errors.As(err, &httpErr) && httpErr.Status >= http.StatusInternalServerError {
		return false, fmt.Errorf("server/discover probe: %w", err)
	}
	if rpcErr := rpcErrorFrom(err); rpcErr != nil && rpcErr.Code == ErrCodeUnsupportedProtocolVersion {
		// Positively modern, but our stateless version is not mutual.
		var data UnsupportedVersionErrorData
		_ = json.Unmarshal(rpcErr.Data, &data)
		for _, v := range data.Supported {
			if EraOfVersion(v) == EraHandshake && IsSupportedProtocolVersion(v) {
				// Dual-era server: the handshake path is mutual.
				return false, nil
			}
		}
		return false, noMutualVersionError(data.Supported)
	}
	if isRecognizedModernError(err) {
		// Modern server rejecting our probe shape; do not fall back to
		// a handshake it will also reject.
		return false, fmt.Errorf("server/discover probe rejected by stateless-era server: %w", err)
	}

	// Anything else (method-not-found, parse junk, timeouts, non-5xx
	// statuses without a modern error body) is not positive evidence
	// of a modern server. Fall back to the legacy handshake.
	r.logger.Debug("server/discover probe fell back to handshake", "server", r.name, "error", err)
	return false, nil
}

// adoptDiscoverResult validates a discover response and, when it is
// positively modern, completes stateless-era setup. A lax legacy server
// that answers unknown methods with junk must not be misclassified:
// adoption requires a well-formed complete result whose
// supportedVersions contain a mutual stateless-era version.
func (r *RPCClient) adoptDiscoverResult(result DiscoverResult) (bool, error) {
	if result.ResultType != ResultTypeComplete || len(result.SupportedVersions) == 0 {
		return false, nil
	}
	var chosen string
	sawStateless := false
	for _, v := range result.SupportedVersions {
		if EraOfVersion(v) == EraStateless {
			sawStateless = true
			if IsSupportedProtocolVersion(v) && chosen == "" {
				chosen = v
			}
		}
	}
	if chosen == "" {
		if !sawStateless {
			// A "discover" result listing only handshake-era versions is
			// not a modern server; treat as legacy junk.
			return false, nil
		}
		return false, noMutualVersionError(result.SupportedVersions)
	}

	if setter, ok := r.transport.(protocolVersionSetter); ok {
		setter.setProtocolVersion(chosen)
	}
	r.SetProtocolVersion(chosen)
	r.SetEra(EraStateless)
	r.SetDownstreamCapabilities(result.Capabilities)
	r.SetInitialized(serverInfoFromMeta(result.Meta))
	r.logger.Info("protocol generation negotiated", "server", r.name, "generation", EraStateless)
	return true, nil
}

// serverInfoFromMeta extracts the spec's serverInfo _meta key from a
// discover result. Missing or malformed info yields a zero ServerInfo,
// which is what lax legacy servers produce today.
func serverInfoFromMeta(meta map[string]any) ServerInfo {
	raw, ok := meta[metaKeyServerInfo]
	if !ok {
		return ServerInfo{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return ServerInfo{}
	}
	var info ServerInfo
	_ = json.Unmarshal(data, &info)
	return info
}

// stampStatelessMeta merges the required stateless-era _meta keys into
// raw request params, preserving any existing _meta entries (the
// tracing injection writes traceparent into the same object). All
// sibling values pass through as raw bytes: a decode through
// map[string]any would rewrite every number as float64 and corrupt
// large integer tool arguments. When the request context carries an
// upstream client's declared capabilities, those are relayed as the
// clientCapabilities value so modern servers know what input requests
// the far end can actually fulfill. Returns params unchanged when they
// are not a JSON object.
func stampStatelessMeta(ctx context.Context, paramsBytes json.RawMessage, version string) json.RawMessage {
	var obj map[string]json.RawMessage
	if len(paramsBytes) > 0 {
		if err := json.Unmarshal(paramsBytes, &obj); err != nil {
			return paramsBytes
		}
	}
	if obj == nil {
		obj = make(map[string]json.RawMessage)
	}
	var meta map[string]json.RawMessage
	if raw, ok := obj["_meta"]; ok {
		_ = json.Unmarshal(raw, &meta)
	}
	if meta == nil {
		meta = make(map[string]json.RawMessage)
	}
	for k, v := range statelessMetaMap(version) {
		raw, err := json.Marshal(v)
		if err != nil {
			return paramsBytes
		}
		meta[k] = raw
	}
	if caps := upstreamClientCapabilitiesFromContext(ctx); len(caps) > 0 {
		meta[metaKeyClientCapabilities] = caps
	}
	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return paramsBytes
	}
	obj["_meta"] = metaRaw

	result, err := json.Marshal(obj)
	if err != nil {
		return paramsBytes
	}
	return result
}
