package tracing

import (
	"context"
	"strings"
	"testing"
	"time"
)

// makeSpan builds a minimal ReadOnlySpan mock from explicit fields.
// We use a real OTel tracer to produce actual spans so ExportSpans is tested
// with the real SDK types.

func TestBufferGetRecent_empty(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	if got := b.GetRecent(5); got != nil {
		t.Errorf("GetRecent on empty buffer = %v, want nil", got)
	}
}

func TestBufferGetByID_missing(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	if got := b.GetByID("not-there"); got != nil {
		t.Errorf("GetByID missing = %v, want nil", got)
	}
}

func TestBufferFilter_empty(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	got := b.Filter(FilterOpts{})
	if len(got) != 0 {
		t.Errorf("Filter on empty buffer len = %d, want 0", len(got))
	}
}

func TestBufferShutdown_noPanic(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	// Manually seed a pending trace (no root span received).
	b.mu.Lock()
	b.pending["fake-trace-id"] = []SpanRecord{
		{
			TraceID:   "fake-trace-id",
			SpanID:    "abc",
			Name:      "test.span",
			StartTime: time.Now().Add(-time.Second),
			EndTime:   time.Now(),
		},
	}
	b.mu.Unlock()

	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown returned error: %v", err)
	}
	if b.Count() != 1 {
		t.Errorf("Count after Shutdown = %d, want 1", b.Count())
	}
}

func TestBufferFilter_errorsOnly(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	// Add one error trace and one success trace manually.
	b.addToBuffer(TraceRecord{
		TraceID:   "t1",
		Operation: "op1",
		StartTime: time.Now(),
		EndTime:   time.Now(),
		IsError:   true,
	})
	b.addToBuffer(TraceRecord{
		TraceID:   "t2",
		Operation: "op2",
		StartTime: time.Now(),
		EndTime:   time.Now(),
		IsError:   false,
	})

	got := b.Filter(FilterOpts{ErrorsOnly: true})
	if len(got) != 1 {
		t.Fatalf("ErrorsOnly filter len = %d, want 1", len(got))
	}
	if got[0].TraceID != "t1" {
		t.Errorf("expected error trace t1, got %s", got[0].TraceID)
	}
}

func TestBufferFilter_serverName(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	b.addToBuffer(TraceRecord{TraceID: "t1", ServerName: "github", StartTime: time.Now(), EndTime: time.Now()})
	b.addToBuffer(TraceRecord{TraceID: "t2", ServerName: "docker", StartTime: time.Now(), EndTime: time.Now()})

	got := b.Filter(FilterOpts{ServerName: "github"})
	if len(got) != 1 || got[0].ServerName != "github" {
		t.Errorf("ServerName filter = %v", got)
	}
}

func TestBufferFilter_minDuration(t *testing.T) {
	b := NewBuffer(10, time.Hour)
	b.addToBuffer(TraceRecord{TraceID: "fast", DurationMs: 10, StartTime: time.Now(), EndTime: time.Now()})
	b.addToBuffer(TraceRecord{TraceID: "slow", DurationMs: 500, StartTime: time.Now(), EndTime: time.Now()})

	got := b.Filter(FilterOpts{MinDuration: 200 * time.Millisecond})
	if len(got) != 1 || got[0].TraceID != "slow" {
		t.Errorf("MinDuration filter = %v", got)
	}
}

func TestBufferFilter_limit(t *testing.T) {
	b := NewBuffer(20, time.Hour)
	for i := 0; i < 10; i++ {
		b.addToBuffer(TraceRecord{TraceID: "t", StartTime: time.Now(), EndTime: time.Now()})
	}
	got := b.Filter(FilterOpts{Limit: 3})
	if len(got) != 3 {
		t.Errorf("Limit filter len = %d, want 3", len(got))
	}
}

func TestBufferRingWrap(t *testing.T) {
	b := NewBuffer(3, time.Hour)
	for i := 0; i < 5; i++ {
		b.addToBuffer(TraceRecord{
			TraceID:   strings.Repeat(string(rune('a'+i)), 32),
			StartTime: time.Now(),
			EndTime:   time.Now(),
		})
	}
	if b.Count() != 3 {
		t.Errorf("Count after wrap = %d, want 3", b.Count())
	}
}

func TestBufferConfig_defaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Sampling != 1.0 {
		t.Errorf("default sampling = %f, want 1.0", cfg.Sampling)
	}
	if cfg.RetentionDuration() != 24*time.Hour {
		t.Errorf("default retention = %v, want 24h", cfg.RetentionDuration())
	}
}

func TestBufferConfig_retentionParse(t *testing.T) {
	cfg := &Config{Retention: "2h"}
	if cfg.RetentionDuration() != 2*time.Hour {
		t.Errorf("retention parse = %v, want 2h", cfg.RetentionDuration())
	}
}

func TestBufferConfig_retentionInvalid(t *testing.T) {
	cfg := &Config{Retention: "not-a-duration"}
	if cfg.RetentionDuration() != 24*time.Hour {
		t.Errorf("invalid retention should fall back to 24h, got %v", cfg.RetentionDuration())
	}
}
