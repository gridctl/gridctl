package metrics

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/tracing"
)

func withManualReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) map[string]metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]metricdata.Metrics{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			out[m.Name] = m
		}
	}
	return out
}

func attrString(set attribute.Set, key attribute.Key) (string, bool) {
	v, ok := set.Value(key)
	if !ok {
		return "", false
	}
	return v.AsString(), true
}

func TestOTelObserver_CompletedCall(t *testing.T) {
	reader := withManualReader(t)
	obs := NewOTelObserver(nil, nil)
	obs.ObserveDispatch(context.Background(), mcp.DispatchObservation{
		ServerName:          "echo",
		ToolName:            "ping",
		ClientName:          "claude",
		HasReplica:          true,
		DownstreamAttempted: true,
		Transport:           tracing.TransportStdio,
		NetworkTransport:    tracing.NetworkPipe,
		MethodName:          tracing.MethodToolsCall,
		OperationName:       tracing.OperationExecuteTool,
		TotalDuration:       1500 * time.Millisecond,
		DownstreamDuration:  800 * time.Millisecond,
		Disposition:         "completed",
		Stage:               "downstream",
		Reason:              "ok",
		HasUsage:            true,
		Usage: mcp.ToolCallSummary{
			InputTokens:         2,
			OutputTokens:        3,
			CacheReadTokens:     4,
			CacheCreationTokens: 5,
		},
	})

	got := collectMetrics(t, reader)
	server, ok := got[tracing.InstrumentServerDuration]
	if !ok || server.Unit != "s" {
		t.Fatalf("server histogram = %+v", server)
	}
	sh := server.Data.(metricdata.Histogram[float64])
	if len(sh.DataPoints) != 1 || sh.DataPoints[0].Count != 1 {
		t.Fatalf("server points = %+v", sh.DataPoints)
	}
	if name, _ := attrString(sh.DataPoints[0].Attributes, tracing.AttrGenAIToolName); name != "ping" {
		t.Fatalf("server tool = %q", name)
	}
	if client, ok := attrString(sh.DataPoints[0].Attributes, tracing.AttrClientName); !ok || client != "claude" {
		t.Fatalf("server client = %q present=%v", client, ok)
	}

	clientHist, ok := got[tracing.InstrumentClientDuration]
	if !ok || clientHist.Unit != "s" {
		t.Fatalf("client histogram = %+v", clientHist)
	}
	ch := clientHist.Data.(metricdata.Histogram[float64])
	if len(ch.DataPoints) != 1 || ch.DataPoints[0].Count != 1 {
		t.Fatalf("client points = %+v", ch.DataPoints)
	}
	if transport, _ := attrString(ch.DataPoints[0].Attributes, tracing.AttrTransport); transport != tracing.TransportStdio {
		t.Fatalf("client transport = %q", transport)
	}

	calls := got[tracing.InstrumentToolCalls]
	if calls.Unit != "{call}" {
		t.Fatalf("calls unit = %q", calls.Unit)
	}
	sum := calls.Data.(metricdata.Sum[int64])
	if len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
		t.Fatalf("calls = %+v", sum.DataPoints)
	}
	if disp, _ := attrString(sum.DataPoints[0].Attributes, tracing.AttrDispatchDisposition); disp != "completed" {
		t.Fatalf("disposition = %q", disp)
	}

	tokens := got[tracing.InstrumentTokens]
	if tokens.Unit != "{token}" {
		t.Fatalf("tokens unit = %q", tokens.Unit)
	}
	tokenSum := tokens.Data.(metricdata.Sum[int64])
	want := map[string]int64{"input": 2, "output": 3, "cache_read": 4, "cache_creation": 5}
	if len(tokenSum.DataPoints) != len(want) {
		t.Fatalf("token points = %+v", tokenSum.DataPoints)
	}
	for _, dp := range tokenSum.DataPoints {
		dir, _ := attrString(dp.Attributes, tracing.AttrUsageDirection)
		if want[dir] != dp.Value {
			t.Fatalf("direction %s = %d, want %d", dir, dp.Value, want[dir])
		}
		if _, ok := attrString(dp.Attributes, tracing.AttrClientName); !ok {
			t.Fatal("completed call omitted client on tokens")
		}
	}
}

func TestOTelObserver_SensitiveOmitsClient(t *testing.T) {
	reader := withManualReader(t)
	obs := NewOTelObserver(nil, nil)
	obs.ObserveDispatch(context.Background(), mcp.DispatchObservation{
		ServerName:          "agent",
		ToolName:            "skill",
		ClientName:          "claude",
		Sensitive:           true,
		HasReplica:          true,
		DownstreamAttempted: true,
		Transport:           tracing.TransportA2A,
		NetworkTransport:    tracing.NetworkTCP,
		MethodName:          tracing.MethodToolsCall,
		OperationName:       tracing.OperationExecuteTool,
		TotalDuration:       time.Second,
		DownstreamDuration:  time.Second,
		Disposition:         "completed",
		HasUsage:            true,
		Usage:               mcp.ToolCallSummary{InputTokens: 1},
	})

	got := collectMetrics(t, reader)
	for _, name := range []string{tracing.InstrumentServerDuration, tracing.InstrumentToolCalls, tracing.InstrumentTokens} {
		m, ok := got[name]
		if !ok {
			t.Fatalf("missing %s", name)
		}
		for _, set := range attributeSets(m) {
			if client, ok := attrString(set, tracing.AttrClientName); ok {
				t.Fatalf("%s carried client %q", name, client)
			}
			if tool, ok := attrString(set, tracing.AttrGenAIToolName); ok && tool != "skill" {
				t.Fatalf("%s tool = %q, want skill", name, tool)
			}
		}
	}
}

func TestOTelObserver_NoClientDurationWithoutDownstreamCall(t *testing.T) {
	reader := withManualReader(t)
	obs := NewOTelObserver(nil, nil)
	obs.ObserveDispatch(context.Background(), mcp.DispatchObservation{
		ServerName:          "echo",
		ToolName:            "ping",
		ClientName:          "claude",
		HasReplica:          true,
		DownstreamAttempted: false,
		MethodName:          tracing.MethodToolsCall,
		OperationName:       tracing.OperationExecuteTool,
		TotalDuration:       20 * time.Millisecond,
		Disposition:         "denied",
		Stage:               "pin",
		Reason:              "schema_pin",
		ErrorType:           "denied",
	})

	got := collectMetrics(t, reader)
	if client, ok := got[tracing.InstrumentClientDuration]; ok {
		h := client.Data.(metricdata.Histogram[float64])
		if len(h.DataPoints) != 0 {
			t.Fatalf("client duration recorded without a downstream call: %+v", h.DataPoints)
		}
	}
	calls := got[tracing.InstrumentToolCalls].Data.(metricdata.Sum[int64])
	if len(calls.DataPoints) != 1 {
		t.Fatalf("calls = %+v", calls.DataPoints)
	}
	if disp, _ := attrString(calls.DataPoints[0].Attributes, tracing.AttrDispatchDisposition); disp != "denied" {
		t.Fatalf("disposition = %q", disp)
	}
	server := got[tracing.InstrumentServerDuration].Data.(metricdata.Histogram[float64])
	if et, _ := attrString(server.DataPoints[0].Attributes, tracing.AttrErrorType); et != "denied" {
		t.Fatalf("error.type = %q", et)
	}
}

func TestOTelObserver_ReplicaCounts(t *testing.T) {
	reader := withManualReader(t)
	obs := NewOTelObserver(func() []ReplicaSample {
		return []ReplicaSample{
			{Server: "echo", State: "healthy"},
			{Server: "echo", State: "healthy"},
			{Server: "echo", State: "healthy"},
			{Server: "echo", State: "unhealthy"},
		}
	}, nil)
	obs.NoteExportFailure(tracing.SignalTraces)

	got := collectMetrics(t, reader)
	gauge, ok := got[tracing.InstrumentReplicas]
	if !ok || gauge.Unit != "{replica}" {
		t.Fatalf("replicas = %+v", gauge)
	}
	points := gauge.Data.(metricdata.Gauge[int64]).DataPoints
	counts := map[string]int64{}
	for _, dp := range points {
		state, _ := attrString(dp.Attributes, tracing.AttrReplicaState)
		server, _ := attrString(dp.Attributes, tracing.AttrServerName)
		if server != "echo" {
			t.Fatalf("server = %q", server)
		}
		counts[state] = dp.Value
	}
	if counts["healthy"] != 3 || counts["unhealthy"] != 1 {
		t.Fatalf("counts = %v", counts)
	}
}

func TestOTelObserver_DropsAndExportFailure(t *testing.T) {
	reader := withManualReader(t)
	obs := NewOTelObserver(nil, func() map[string]uint64 {
		return map[string]uint64{"queue_full": 2}
	})
	obs.NoteExportFailure(tracing.SignalTraces)
	obs.NoteExportFailure(tracing.SignalMetrics)
	obs.NoteExportFailure(tracing.SignalUnknown)

	got := collectMetrics(t, reader)
	drops, ok := got[tracing.InstrumentRunsDropped]
	if !ok || drops.Unit != "{record}" {
		t.Fatalf("drops = %+v", drops)
	}
	points := drops.Data.(metricdata.Sum[int64]).DataPoints
	if len(points) != 1 || points[0].Value != 2 {
		t.Fatalf("drop points = %+v", points)
	}
	if reason, _ := attrString(points[0].Attributes, tracing.AttrRunsDropReason); reason != "queue_full" {
		t.Fatalf("drop reason = %q", reason)
	}

	failures := got[tracing.InstrumentExportFailures]
	if failures.Unit != "{failure}" {
		t.Fatalf("failures unit = %q", failures.Unit)
	}
	seen := map[string]int64{}
	for _, dp := range failures.Data.(metricdata.Sum[int64]).DataPoints {
		signal, _ := attrString(dp.Attributes, tracing.AttrOTLPSignal)
		seen[signal] = dp.Value
	}
	if seen[tracing.SignalTraces] != 1 || seen[tracing.SignalMetrics] != 1 || seen[tracing.SignalUnknown] != 1 {
		t.Fatalf("failure signals = %v", seen)
	}
}

func TestOTelObserver_NilDropsOmitsInstrument(t *testing.T) {
	reader := withManualReader(t)
	obs := NewOTelObserver(nil, nil)
	obs.NoteExportFailure(tracing.SignalTraces)
	got := collectMetrics(t, reader)
	if _, ok := got[tracing.InstrumentRunsDropped]; ok {
		t.Fatal("nil drops callback registered gridctl.runs.dropped")
	}
}

func attributeSets(m metricdata.Metrics) []attribute.Set {
	switch data := m.Data.(type) {
	case metricdata.Histogram[float64]:
		out := make([]attribute.Set, len(data.DataPoints))
		for i, dp := range data.DataPoints {
			out[i] = dp.Attributes
		}
		return out
	case metricdata.Sum[int64]:
		out := make([]attribute.Set, len(data.DataPoints))
		for i, dp := range data.DataPoints {
			out[i] = dp.Attributes
		}
		return out
	default:
		return nil
	}
}
