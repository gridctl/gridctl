package mcp

import (
	"context"
	"net/url"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/tracing"
)

type toolCallFinish struct {
	started           time.Time
	requestedName     string
	bareTool          string
	rpcCode           *int
	downstreamIsError bool
	sensitive         bool
	sensitiveCategory string
	resolvedServer    string
	resolvedTool      string
	replicaID         int
	hasReplica        bool
	transport         transportAttrs
	downstreamDur     time.Duration
	usage             ToolCallSummary
	hasUsage          bool
}

func toolsCallSpanName(tool string) string {
	tool = logging.RedactString(tool)
	if tool == "" {
		return tracing.SpanNameToolsCall
	}
	return tracing.SpanNameToolsCall + " " + tool
}

func stampCallError(span trace.Span, in ErrorTypeInput) {
	if span == nil || !span.IsRecording() {
		return
	}
	if in.RPCCode != nil {
		span.SetAttributes(attribute.Int(string(tracing.AttrRPCResponseStatus), *in.RPCCode))
	}
	if et := SelectErrorType(in); et != "" {
		span.SetAttributes(attribute.String(string(tracing.AttrErrorType), et))
	}
}

func (g *Gateway) finishToolCallSpan(ctx context.Context, root trace.Span, state *callState, fin toolCallFinish) {
	in := ErrorTypeInput{
		RPCCode:     fin.rpcCode,
		ToolIsError: fin.downstreamIsError,
		Disposition: state.outcome.Disposition,
		Reason:      state.outcome.Reason,
	}
	errorType := SelectErrorType(in)
	if root != nil && root.IsRecording() {
		if fin.sensitive {
			root.SetName(toolsCallSpanName(fin.sensitiveCategory))
			tracing.SetAttributesWithAliases(root,
				attribute.String(string(tracing.AttrGenAIToolName), fin.sensitiveCategory),
			)
		} else if clientID := attributionID(ctx); clientID != "" {
			tracing.SetAttributesWithAliases(root,
				attribute.String(string(tracing.AttrClientName), logging.RedactString(clientID)),
			)
		}
		if errorType != "" {
			root.SetAttributes(attribute.String(string(tracing.AttrErrorType), errorType))
		}
		if fin.rpcCode != nil {
			root.SetAttributes(attribute.Int(string(tracing.AttrRPCResponseStatus), *fin.rpcCode))
		}
	}
	g.notifyDispatch(ctx, fin, state.outcome, errorType)
}

func (g *Gateway) notifyDispatch(ctx context.Context, fin toolCallFinish, outcome CallOutcome, errorType string) {
	g.mu.RLock()
	obs := g.dispatchObserver
	g.mu.RUnlock()
	if obs == nil {
		return
	}
	tool := fin.resolvedTool
	serverName := fin.resolvedServer
	if tool == "" {
		tool = fin.bareTool
	}
	if serverName == "" {
		if server, parsed, err := ParsePrefixedTool(fin.requestedName); err == nil {
			serverName = server
			if tool == "" {
				tool = parsed
			}
		}
	}
	clientName := logging.RedactString(attributionID(ctx))
	server := logging.RedactString(serverName)
	if fin.sensitive {
		tool = fin.sensitiveCategory
		clientName = ""
	} else {
		tool = logging.RedactString(tool)
	}
	record := DispatchObservation{
		ServerName:         server,
		ToolName:           tool,
		ClientName:         clientName,
		ReplicaID:          fin.replicaID,
		HasReplica:         fin.hasReplica,
		Transport:          fin.transport.Gridctl,
		NetworkTransport:   fin.transport.Network,
		ServerAddress:      fin.transport.Address,
		ServerPort:         fin.transport.Port,
		HasServerPort:      fin.transport.HasPort,
		MethodName:         tracing.MethodToolsCall,
		OperationName:      tracing.OperationExecuteTool,
		ProtocolVersion:    upstreamProtocolVersion(ctx),
		TotalDuration:      time.Since(fin.started),
		DownstreamDuration: fin.downstreamDur,
		Disposition:        outcome.Disposition,
		Stage:              outcome.Stage,
		Reason:             outcome.Reason,
		ErrorType:          errorType,
		Sensitive:          fin.sensitive,
		Usage:              fin.usage,
		HasUsage:           fin.hasUsage,
	}
	obs.ObserveDispatch(ctx, record)
}

type transportAttrs struct {
	Gridctl string
	Network string
	Address string
	Port    int
	HasPort bool
}

// resolveTransportAttrs derives gridctl.transport, network.transport, and
// the server address/port from the same inputs resolveNetworkTransport used.
// network.transport is pipe for stdio and process and tcp otherwise. SSH
// address and port come from SSHHost and SSHPort (0 means the default 22).
func resolveTransportAttrs(cfg MCPServerConfig, hasMeta bool) transportAttrs {
	out := transportAttrs{Gridctl: tracing.TransportHTTP, Network: tracing.NetworkTCP}
	if !hasMeta {
		applyEndpoint(&out, cfg.Endpoint)
		return out
	}
	switch {
	case cfg.SSH:
		out.Gridctl = tracing.TransportSSH
		out.Address = cfg.SSHHost
		port := cfg.SSHPort
		if port == 0 {
			port = 22
		}
		out.Port = port
		out.HasPort = cfg.SSHHost != ""
	case cfg.LocalProcess:
		out.Gridctl = tracing.TransportProcess
		out.Network = tracing.NetworkPipe
	case cfg.OpenAPI:
		out.Gridctl = tracing.TransportOpenAPI
		if cfg.OpenAPIConfig != nil {
			applyEndpoint(&out, cfg.OpenAPIConfig.BaseURL)
		}
		if out.Address == "" {
			applyEndpoint(&out, cfg.Endpoint)
		}
	case cfg.A2A:
		out.Gridctl = tracing.TransportA2A
		if cfg.A2AConfig != nil {
			applyEndpoint(&out, cfg.A2AConfig.Endpoint)
		}
		if out.Address == "" {
			applyEndpoint(&out, cfg.Endpoint)
		}
	case cfg.Transport == TransportStdio:
		out.Gridctl = tracing.TransportStdio
		out.Network = tracing.NetworkPipe
	case cfg.Transport == TransportSSE:
		out.Gridctl = tracing.TransportSSE
		applyEndpoint(&out, cfg.Endpoint)
	default:
		applyEndpoint(&out, cfg.Endpoint)
	}
	return out
}

func applyEndpoint(out *transportAttrs, raw string) {
	if raw == "" || out == nil {
		return
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return
	}
	out.Address = u.Hostname()
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err == nil {
			out.Port = n
			out.HasPort = true
		}
	}
}
