package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/tracing"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/mock/gomock"
)

func TestPrincipal_ObservationAttribution(t *testing.T) {
	buffer := tracing.NewBuffer(10, time.Hour)
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(buffer)))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	dir := t.TempDir()
	recorder, err := runs.NewRecorder(runs.Config{
		Enabled: true, Dir: dir, MaxBytes: 1 << 20, MaxAge: time.Hour,
		QueueSize: 8, SyncInterval: time.Millisecond, ShutdownDrain: time.Second,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(recorder.Close)
	g := NewGateway()
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	g.SetRunSink(recSink{rec: recorder})
	observer := &recordingObserver{}
	g.SetToolCallObserver(observer)
	client := setupMockAgentClient(gomock.NewController(t), "alpha", []Tool{{Name: "echo"}})
	client.EXPECT().CallTool(gomock.Any(), "echo", gomock.Any()).Return(&ToolCallResult{Content: []Content{NewTextContent("ok")}}, nil)
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	ctx := WithPrincipal(context.Background(), testPrincipal)
	ctx = WithClientAccessID(WithClientID(ctx, "cursor"), testPrincipal)
	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "alpha__echo"})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.NotContains(t, logs.String(), testPrincipal)
	require.Equal(t, "cursor", ClientIDFromContext(ctx))
	require.Len(t, observer.calls, 1)
	require.Equal(t, testPrincipal, observer.calls[0].ClientID)
	require.Equal(t, 1, buffer.Count())
	found := false
	for _, span := range buffer.GetRecent(1)[0].Spans {
		if value, ok := span.Attrs["mcp.client.name"]; ok {
			require.Equal(t, testPrincipal, value)
			found = true
		}
	}
	require.True(t, found, "trace must carry authenticated attribution")
	require.Eventually(t, func() bool {
		result, err := runs.Query(context.Background(), dir, runs.Filter{}, 10, nil, recorder.WipeEpoch())
		if err != nil || len(result.Records) != 1 {
			return false
		}
		require.Equal(t, testPrincipal, result.Records[0].AccessLabel)
		require.Equal(t, "cursor", result.Records[0].ClientLabel)
		return true
	}, time.Second, time.Millisecond)
}
