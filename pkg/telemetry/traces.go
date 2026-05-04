package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/natefinch/lumberjack.v2"
)

// serverNameAttrKey is the OTel attribute used today by the gateway tracer
// to tag every server-bound span (see pkg/tracing). It mirrors the
// well-known `server.name` semantic convention.
const serverNameAttrKey = "server.name"

// TracesFileClient implements otlptrace.Client by writing each batch as one
// OTLP-JSON line per configured server. The OTel SDK calls UploadTraces with
// a slice of *tracepb.ResourceSpans already converted from ReadOnlySpan, so
// this avoids re-implementing the SDK's transform code.
//
// Each emitted line is a fully valid TracesData envelope, matching the OTLP
// File Exporter spec — `tail -f traces.jsonl | otelcol --config ...
// otlpjsonfilereceiver` ingests cleanly.
//
// Spans without a resolvable server.name attribute (rare; the gateway
// stamps it on every tool-call span) are dropped. This is documented as an
// accepted Phase-2 limitation: per-server file routing is what users want,
// and a "miscellaneous" file would muddy the inventory model.
type TracesFileClient struct {
	mu      sync.RWMutex
	writers map[string]*lumberjack.Logger
	logger  *slog.Logger
}

// NewTracesFileClient constructs an empty client. Add servers via AddServer
// before passing the client to otlptrace.NewUnstarted.
func NewTracesFileClient() *TracesFileClient {
	return &TracesFileClient{
		writers: make(map[string]*lumberjack.Logger),
	}
}

// SetLogger configures where the client logs persistence errors.
func (c *TracesFileClient) SetLogger(logger *slog.Logger) {
	if logger != nil {
		c.logger = logger.With("subsystem", "telemetry")
	}
}

// AddServer registers a per-server traces.jsonl file. Idempotent: re-adding
// a server replaces the prior writer (the lumberjack handle is closed).
func (c *TracesFileClient) AddServer(name, path string, opts LogOpts) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.writers[name]; ok && existing != nil {
		_ = existing.Close()
	}

	if opts.MaxSizeMB <= 0 {
		opts.MaxSizeMB = 100
	}
	if opts.MaxBackups <= 0 {
		opts.MaxBackups = 5
	}
	if opts.MaxAgeDays <= 0 {
		opts.MaxAgeDays = 7
	}

	if err := touchMode0600(path); err != nil {
		return fmt.Errorf("telemetry traces writer for %q: %w", name, err)
	}

	c.writers[name] = &lumberjack.Logger{
		Filename:   path,
		MaxSize:    opts.MaxSizeMB,
		MaxBackups: opts.MaxBackups,
		MaxAge:     opts.MaxAgeDays,
		Compress:   true,
	}
	return nil
}

// RemoveServer stops persisting traces for a server and closes its writer.
func (c *TracesFileClient) RemoveServer(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.writers[name]; ok && existing != nil {
		_ = existing.Close()
	}
	delete(c.writers, name)
}

// ConfiguredServers returns the names currently persisting traces.
func (c *TracesFileClient) ConfiguredServers() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.writers))
	for n := range c.writers {
		names = append(names, n)
	}
	return names
}

// Start implements otlptrace.Client. No connection setup required for files.
func (c *TracesFileClient) Start(_ context.Context) error { return nil }

// Stop implements otlptrace.Client. Closes every per-server writer. Safe to
// call multiple times.
func (c *TracesFileClient) Stop(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, lj := range c.writers {
		if lj != nil {
			_ = lj.Close()
		}
	}
	c.writers = make(map[string]*lumberjack.Logger)
	return nil
}

// UploadTraces implements otlptrace.Client. Re-routes spans by server.name
// attribute and writes one TracesData envelope per server per call. The
// per-server writer map is snapshotted under the lock; disk I/O happens
// outside the lock so a slow writer can't block AddServer/RemoveServer/Stop.
// Errors from individual writers are logged and swallowed — the SDK's batch
// processor must not stall because a disk write failed.
func (c *TracesFileClient) UploadTraces(_ context.Context, resourceSpans []*tracepb.ResourceSpans) error {
	if len(resourceSpans) == 0 {
		return nil
	}

	// Group spans by server.name. Each group keeps the resource and scope
	// metadata of the source ResourceSpans so the line stays a faithful
	// OTLP envelope; only the span set is partitioned.
	type bucket struct {
		spans []*tracepb.Span
		scope *commonpb.InstrumentationScope
	}
	perServer := make(map[string]map[*tracepb.ResourceSpans]*bucket) // server -> rs -> bucket

	for _, rs := range resourceSpans {
		for _, ss := range rs.ScopeSpans {
			for _, span := range ss.Spans {
				server := extractServerNameFromAttrs(span.Attributes)
				if server == "" {
					continue
				}
				rsMap, ok := perServer[server]
				if !ok {
					rsMap = make(map[*tracepb.ResourceSpans]*bucket)
					perServer[server] = rsMap
				}
				b, ok := rsMap[rs]
				if !ok {
					b = &bucket{scope: ss.Scope}
					rsMap[rs] = b
				}
				b.spans = append(b.spans, span)
			}
		}
	}

	type planned struct {
		writer *lumberjack.Logger
		server string
		data   []byte
	}
	var plan []planned

	c.mu.RLock()
	for server, rsMap := range perServer {
		writer, ok := c.writers[server]
		if !ok {
			continue
		}
		envelope := &tracepb.TracesData{}
		for rs, b := range rsMap {
			envelope.ResourceSpans = append(envelope.ResourceSpans, &tracepb.ResourceSpans{
				Resource: rs.Resource,
				ScopeSpans: []*tracepb.ScopeSpans{{
					Scope: b.scope,
					Spans: b.spans,
				}},
				SchemaUrl: rs.SchemaUrl,
			})
		}
		data, err := protojson.Marshal(envelope)
		if err != nil {
			c.logWarn("telemetry traces marshal failed", "server", server, "error", err)
			continue
		}
		data = append(data, '\n')
		plan = append(plan, planned{writer: writer, server: server, data: data})
	}
	c.mu.RUnlock()

	for _, p := range plan {
		if _, err := p.writer.Write(p.data); err != nil {
			c.logWarn("telemetry traces write failed", "server", p.server, "error", err)
		}
	}
	return nil
}

func (c *TracesFileClient) logWarn(msg string, args ...any) {
	if c.logger != nil {
		c.logger.Warn(msg, args...)
	}
}

// extractServerNameFromAttrs walks an attribute slice and returns the value
// of `server.name` if present. Match strategy is by key only — proto
// AnyValue strings are accepted; non-string values are coerced to "".
func extractServerNameFromAttrs(attrs []*commonpb.KeyValue) string {
	for _, kv := range attrs {
		if kv.Key != serverNameAttrKey {
			continue
		}
		if kv.Value == nil {
			return ""
		}
		if sv, ok := kv.Value.Value.(*commonpb.AnyValue_StringValue); ok {
			return sv.StringValue
		}
	}
	return ""
}

// Verify that TracesFileClient satisfies the otlptrace.Client interface at
// compile time. otlptrace.NewUnstarted accepts any Client.
var _ otlptrace.Client = (*TracesFileClient)(nil)
