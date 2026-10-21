package metrics

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/tracing"
)

// ReplicaSample is one replica row for the observable gauge.
type ReplicaSample struct {
	Server string
	State  string
}

// OTelObserver records dispatch observations on the global meter. It does not
// own the SDK; the tracing provider does. A nil meter provider records into
// the default noop meter.
type OTelObserver struct {
	replicas func() []ReplicaSample
	drops    func() map[string]uint64

	once     sync.Once
	server   metric.Float64Histogram
	client   metric.Float64Histogram
	calls    metric.Int64Counter
	tokens   metric.Int64Counter
	failures metric.Int64Counter
}

// NewOTelObserver returns a dispatch observer. replicas and drops may be nil.
// A nil drops callback omits gridctl.runs.dropped.
func NewOTelObserver(replicas func() []ReplicaSample, drops func() map[string]uint64) *OTelObserver {
	return &OTelObserver{replicas: replicas, drops: drops}
}

// ObserveDispatch implements mcp.DispatchObserver.
func (o *OTelObserver) ObserveDispatch(ctx context.Context, obs mcp.DispatchObservation) {
	if o == nil {
		return
	}
	o.ensure()
	serverAttrs := []attribute.KeyValue{
		attribute.String(string(tracing.AttrMethodName), obs.MethodName),
		attribute.String(string(tracing.AttrGenAIOperationName), obs.OperationName),
		attribute.String(string(tracing.AttrGenAIToolName), logging.RedactString(obs.ToolName)),
		attribute.String(string(tracing.AttrServerName), logging.RedactString(obs.ServerName)),
	}
	if obs.ClientName != "" && !obs.Sensitive {
		serverAttrs = append(serverAttrs, attribute.String(string(tracing.AttrClientName), logging.RedactString(obs.ClientName)))
	}
	if obs.ErrorType != "" {
		serverAttrs = append(serverAttrs, attribute.String(string(tracing.AttrErrorType), obs.ErrorType))
	}
	o.server.Record(ctx, obs.TotalDuration.Seconds(), metric.WithAttributes(serverAttrs...))

	if obs.HasReplica {
		clientAttrs := []attribute.KeyValue{
			attribute.String(string(tracing.AttrMethodName), obs.MethodName),
			attribute.String(string(tracing.AttrGenAIOperationName), obs.OperationName),
			attribute.String(string(tracing.AttrGenAIToolName), logging.RedactString(obs.ToolName)),
			attribute.String(string(tracing.AttrServerName), logging.RedactString(obs.ServerName)),
			attribute.String(string(tracing.AttrTransport), obs.Transport),
			attribute.String(string(tracing.AttrNetworkTransport), obs.NetworkTransport),
		}
		if obs.ServerAddress != "" {
			clientAttrs = append(clientAttrs, attribute.String(string(tracing.AttrServerAddress), logging.RedactString(obs.ServerAddress)))
		}
		if obs.HasServerPort {
			clientAttrs = append(clientAttrs, attribute.Int(string(tracing.AttrServerPort), obs.ServerPort))
		}
		if obs.ErrorType != "" {
			clientAttrs = append(clientAttrs, attribute.String(string(tracing.AttrErrorType), obs.ErrorType))
		}
		o.client.Record(ctx, obs.DownstreamDuration.Seconds(), metric.WithAttributes(clientAttrs...))
	}

	callAttrs := []attribute.KeyValue{
		attribute.String(string(tracing.AttrServerName), logging.RedactString(obs.ServerName)),
		attribute.String(string(tracing.AttrGenAIToolName), logging.RedactString(obs.ToolName)),
		attribute.String(string(tracing.AttrDispatchDisposition), obs.Disposition),
		attribute.String(string(tracing.AttrDispatchStage), obs.Stage),
		attribute.String(string(tracing.AttrDispatchReason), obs.Reason),
	}
	if obs.ClientName != "" && !obs.Sensitive {
		callAttrs = append(callAttrs, attribute.String(string(tracing.AttrClientName), logging.RedactString(obs.ClientName)))
	}
	o.calls.Add(ctx, 1, metric.WithAttributes(callAttrs...))

	if obs.HasUsage {
		o.addTokens(ctx, obs, "input", obs.Usage.InputTokens)
		o.addTokens(ctx, obs, "output", obs.Usage.OutputTokens)
		o.addTokens(ctx, obs, "cache_read", obs.Usage.CacheReadTokens)
		o.addTokens(ctx, obs, "cache_creation", obs.Usage.CacheCreationTokens)
	}
}

// NoteExportFailure increments gridctl.otlp.export.failures. The tracing
// error handler calls this.
func (o *OTelObserver) NoteExportFailure(signal string) {
	if o == nil || signal == "" {
		return
	}
	o.ensure()
	o.failures.Add(context.Background(), 1, metric.WithAttributes(
		attribute.String(string(tracing.AttrOTLPSignal), signal),
	))
}

func (o *OTelObserver) addTokens(ctx context.Context, obs mcp.DispatchObservation, direction string, n int) {
	if n <= 0 {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String(string(tracing.AttrUsageDirection), direction),
		attribute.String(string(tracing.AttrServerName), logging.RedactString(obs.ServerName)),
		attribute.String(string(tracing.AttrGenAIToolName), logging.RedactString(obs.ToolName)),
	}
	if obs.ClientName != "" && !obs.Sensitive {
		attrs = append(attrs, attribute.String(string(tracing.AttrClientName), logging.RedactString(obs.ClientName)))
	}
	o.tokens.Add(ctx, int64(n), metric.WithAttributes(attrs...))
}

func (o *OTelObserver) ensure() {
	o.once.Do(func() {
		meter := otel.Meter("gridctl.gateway")
		o.server, _ = meter.Float64Histogram(tracing.InstrumentServerDuration, metric.WithUnit("s"))
		o.client, _ = meter.Float64Histogram(tracing.InstrumentClientDuration, metric.WithUnit("s"))
		o.calls, _ = meter.Int64Counter(tracing.InstrumentToolCalls, metric.WithUnit("{call}"))
		o.tokens, _ = meter.Int64Counter(tracing.InstrumentTokens, metric.WithUnit("{token}"))
		o.failures, _ = meter.Int64Counter(tracing.InstrumentExportFailures, metric.WithUnit("{failure}"))
		o.registerObservables(meter)
	})
}

func (o *OTelObserver) registerObservables(meter metric.Meter) {
	var instruments []metric.Observable
	var replicas metric.Int64ObservableGauge
	if o.replicas != nil {
		g, err := meter.Int64ObservableGauge(tracing.InstrumentReplicas, metric.WithUnit("{replica}"))
		if err == nil {
			replicas = g
			instruments = append(instruments, g)
		}
	}
	var drops metric.Int64ObservableCounter
	if o.drops != nil {
		c, err := meter.Int64ObservableCounter(tracing.InstrumentRunsDropped, metric.WithUnit("{record}"))
		if err == nil {
			drops = c
			instruments = append(instruments, c)
		}
	}
	if len(instruments) == 0 {
		return
	}
	_, _ = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		if replicas != nil && o.replicas != nil {
			for _, sample := range o.replicas() {
				observer.ObserveInt64(replicas, 1, metric.WithAttributes(
					attribute.String(string(tracing.AttrServerName), logging.RedactString(sample.Server)),
					attribute.String(string(tracing.AttrReplicaState), sample.State),
				))
			}
		}
		if drops != nil && o.drops != nil {
			for reason, n := range o.drops() {
				observer.ObserveInt64(drops, int64(n), metric.WithAttributes(
					attribute.String(string(tracing.AttrRunsDropReason), reason),
				))
			}
		}
		return nil
	}, instruments...)
}
