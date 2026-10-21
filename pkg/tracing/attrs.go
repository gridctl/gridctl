package tracing

import (
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// AliasRemovalRelease is the release that drops legacy attribute keys and
// the old network.transport values. Both are emitted until that release.
// Diagnostic output, not a versioned contract.
const AliasRemovalRelease = "1.1.0"

// aliasesEnabled gates the deprecation window. Flip to false in
// AliasRemovalRelease and delete the legacy stamps in the same change.
const aliasesEnabled = true

// Semantic-convention keys. Development status; kept in this file so a
// registry rename is one edit.
const (
	AttrMethodName         = attribute.Key("mcp.method.name")
	AttrProtocolVersion    = attribute.Key("mcp.protocol.version")
	AttrGenAIToolName      = attribute.Key("gen_ai.tool.name")
	AttrGenAIOperationName = attribute.Key("gen_ai.operation.name")
	AttrErrorType          = attribute.Key("error.type")
	AttrRPCResponseStatus  = attribute.Key("rpc.response.status_code")
	AttrNetworkTransport   = attribute.Key("network.transport")
	AttrServerAddress      = attribute.Key("server.address")
	AttrServerPort         = attribute.Key("server.port")
	AttrServiceName        = attribute.Key("service.name")
	AttrServiceVersion     = attribute.Key("service.version")
	AttrServiceInstanceID  = attribute.Key("service.instance.id")
)

// Gridctl keys. Inventions that used to sit in reserved namespaces.
const (
	AttrServerName          = attribute.Key("gridctl.server.name")
	AttrReplicaID           = attribute.Key("gridctl.replica.id")
	AttrClientName          = attribute.Key("gridctl.client.name")
	AttrTransport           = attribute.Key("gridctl.transport")
	AttrProtocolGeneration  = attribute.Key("gridctl.protocol.generation")
	AttrOutputFormat        = attribute.Key("gridctl.output.format")
	AttrAutoscaleDirection  = attribute.Key("gridctl.autoscale.direction")
	AttrAutoscaleReason     = attribute.Key("gridctl.autoscale.reason")
	AttrUsageInputTokens    = attribute.Key("gridctl.usage.input_tokens")
	AttrUsageOutputTokens   = attribute.Key("gridctl.usage.output_tokens")
	AttrUsageCacheRead      = attribute.Key("gridctl.usage.cache_read_tokens")
	AttrUsageCacheCreation  = attribute.Key("gridctl.usage.cache_creation_tokens")
	AttrUsageDirection      = attribute.Key("gridctl.usage.direction")
	AttrDispatchDisposition = attribute.Key("gridctl.dispatch.disposition")
	AttrDispatchStage       = attribute.Key("gridctl.dispatch.stage")
	AttrDispatchReason      = attribute.Key("gridctl.dispatch.reason")
	AttrReplicaState        = attribute.Key("gridctl.replica.state")
	AttrRunsDropReason      = attribute.Key("gridctl.runs.drop_reason")
	AttrOTLPSignal          = attribute.Key("gridctl.otlp.signal")
)

// Legacy keys as emitted before the rename. Readers fall back to these
// permanently because seeded traces keep them.
const (
	AttrLegacyServerName         = attribute.Key("server.name")
	AttrLegacyMCPServerName      = attribute.Key("mcp.server.name")
	AttrLegacyMCPServer          = attribute.Key("mcp.server")
	AttrLegacyReplicaID          = attribute.Key("mcp.replica.id")
	AttrLegacyClientName         = attribute.Key("mcp.client.name")
	AttrLegacyProtocolGeneration = attribute.Key("mcp.protocol.generation")
	AttrLegacyToolName           = attribute.Key("mcp.tool.name")
	AttrLegacyBareToolName       = attribute.Key("tool.name")
	AttrLegacyOutputFormat       = attribute.Key("output.format")
	AttrLegacyAutoscaleDirection = attribute.Key("mcp.autoscale.direction")
	AttrLegacyAutoscaleReason    = attribute.Key("mcp.autoscale.reason")
	AttrLegacyUsageInput         = attribute.Key("gen_ai.usage.input_tokens")
	AttrLegacyUsageOutput        = attribute.Key("gen_ai.usage.output_tokens")
	AttrLegacyUsageCacheRead     = attribute.Key("gen_ai.usage.cache_read.input_tokens")
	AttrLegacyUsageCacheCreation = attribute.Key("gen_ai.usage.cache_creation.input_tokens")
)

// Operation and method values.
const (
	MethodToolsCall      = "tools/call"
	OperationExecuteTool = "execute_tool"
	SpanNameToolsCall    = "tools/call"
)

// Network and gridctl transport values.
const (
	NetworkPipe = "pipe"
	NetworkTCP  = "tcp"

	TransportStdio   = "stdio"
	TransportProcess = "process"
	TransportSSH     = "ssh"
	TransportHTTP    = "http"
	TransportSSE     = "sse"
	TransportOpenAPI = "openapi"
	TransportA2A     = "a2a"
)

// OTLP signal names for the export-failure counter.
const (
	SignalTraces      = "traces"
	SignalMetrics     = "metrics"
	SignalPathTraces  = "/v1/traces"
	SignalPathMetrics = "/v1/metrics"
)

// Instrument names.
const (
	InstrumentServerDuration = "mcp.server.operation.duration"
	InstrumentClientDuration = "mcp.client.operation.duration"
	InstrumentToolCalls      = "gridctl.gateway.tool.calls"
	InstrumentTokens         = "gridctl.gateway.tokens" //nolint:gosec // instrument name, not a credential
	InstrumentReplicas       = "gridctl.gateway.replicas"
	InstrumentRunsDropped    = "gridctl.runs.dropped"
	InstrumentExportFailures = "gridctl.otlp.export.failures"
)

// DurationHistogramBuckets are the MCP operation-duration boundaries, in seconds.
var DurationHistogramBuckets = []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 60, 120, 300}

// LegacyAliases maps each new key to the old keys emitted for the same value.
// A new key may alias more than one old key. network.transport is absent:
// the key stays and only its values change.
var LegacyAliases = map[attribute.Key][]attribute.Key{
	AttrServerName:         {AttrLegacyServerName, AttrLegacyMCPServerName, AttrLegacyMCPServer},
	AttrReplicaID:          {AttrLegacyReplicaID},
	AttrClientName:         {AttrLegacyClientName},
	AttrProtocolGeneration: {AttrLegacyProtocolGeneration},
	AttrGenAIToolName:      {AttrLegacyToolName, AttrLegacyBareToolName},
	AttrOutputFormat:       {AttrLegacyOutputFormat},
	AttrAutoscaleDirection: {AttrLegacyAutoscaleDirection},
	AttrAutoscaleReason:    {AttrLegacyAutoscaleReason},
	AttrUsageInputTokens:   {AttrLegacyUsageInput},
	AttrUsageOutputTokens:  {AttrLegacyUsageOutput},
	AttrUsageCacheRead:     {AttrLegacyUsageCacheRead},
	AttrUsageCacheCreation: {AttrLegacyUsageCacheCreation},
}

// AliasesEnabled reports whether the deprecation window is still open.
func AliasesEnabled() bool { return aliasesEnabled }

// SetAttributesWithAliases stamps attrs and, while the window is open, each
// legacy alias of those keys with the same value.
func SetAttributesWithAliases(span trace.Span, attrs ...attribute.KeyValue) {
	if span == nil || !span.IsRecording() || len(attrs) == 0 {
		return
	}
	if !aliasesEnabled {
		span.SetAttributes(attrs...)
		return
	}
	out := make([]attribute.KeyValue, 0, len(attrs)*3)
	for _, kv := range attrs {
		out = append(out, kv)
		for _, old := range LegacyAliases[kv.Key] {
			out = append(out, attribute.KeyValue{Key: old, Value: kv.Value})
		}
	}
	span.SetAttributes(out...)
}

// FirstString returns the first non-empty value among keys. Readers use this
// so a new key wins and a seeded old key still resolves.
func FirstString(attrs map[string]string, keys ...string) string {
	if len(attrs) == 0 {
		return ""
	}
	for _, key := range keys {
		if v := attrs[key]; v != "" {
			return v
		}
	}
	return ""
}

// ServerNameKeys is the fallback order for a server name attribute.
func ServerNameKeys() []string {
	return []string{
		string(AttrServerName),
		string(AttrLegacyServerName),
		string(AttrLegacyMCPServerName),
		string(AttrLegacyMCPServer),
	}
}

// ToolNameKeys is the fallback order for a tool name attribute.
func ToolNameKeys() []string {
	return []string{
		string(AttrGenAIToolName),
		string(AttrLegacyToolName),
		string(AttrLegacyBareToolName),
	}
}

// ClientNameKeys is the fallback order for a client name attribute.
func ClientNameKeys() []string {
	return []string{string(AttrClientName), string(AttrLegacyClientName)}
}

// TransportKeys is the fallback order for the transport label. The key did
// not change; readers still look at network.transport.
func TransportKeys() []string {
	return []string{string(AttrNetworkTransport), string(AttrTransport)}
}
