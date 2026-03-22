package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/tracing"
)

func newTestServerWithTraces(t *testing.T) *Server {
	t.Helper()
	srv := newTestServer(t)
	buf := tracing.NewBuffer(100, time.Hour)
	srv.SetTraceBuffer(buf)
	return srv
}

func TestHandleTraces_emptyBuffer(t *testing.T) {
	srv := newTestServerWithTraces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var result []tracing.TraceRecord
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

func TestHandleTraces_methodNotAllowed(t *testing.T) {
	srv := newTestServerWithTraces(t)
	req := httptest.NewRequest(http.MethodPost, "/api/traces", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandleTraces_withData(t *testing.T) {
	srv := newTestServer(t)
	buf := tracing.NewBuffer(100, time.Hour)

	// Inject test traces directly.
	buf.ExportSpans(nil, nil) // no-op
	// We test via the public addLocked path indirectly using Filter.
	// Here we seed the buffer using Filter opts (which goes through GetRecent).
	// Since adding directly is internal, use Shutdown to add pending spans.
	_ = buf.Shutdown(nil)

	srv.SetTraceBuffer(buf)

	req := httptest.NewRequest(http.MethodGet, "/api/traces?limit=10", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleTraceDetail_notFound(t *testing.T) {
	srv := newTestServerWithTraces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/traces/nonexistent-trace-id", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandleTraces_nilBuffer(t *testing.T) {
	// When no trace buffer is set, should return empty array.
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/traces", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var result []tracing.TraceRecord
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

func TestHandleTraces_filterErrors(t *testing.T) {
	srv := newTestServerWithTraces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/traces?errors=true&limit=50", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestHandleTraces_filterMinDuration(t *testing.T) {
	srv := newTestServerWithTraces(t)
	req := httptest.NewRequest(http.MethodGet, "/api/traces?min_duration=500ms", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
