package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/tracing"
)

// handleTraces handles GET /api/traces and GET /api/traces/{traceId}.
func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.traceBuffer == nil {
		writeJSON(w, []tracing.TraceRecord{})
		return
	}

	// Check for trace ID in path: /api/traces/{traceId}
	path := strings.TrimPrefix(r.URL.Path, "/api/traces")
	path = strings.TrimPrefix(path, "/")
	if path != "" {
		s.handleTraceDetail(w, r, path)
		return
	}

	// List with optional filters.
	opts := tracing.FilterOpts{
		ServerName: r.URL.Query().Get("server"),
		ErrorsOnly: r.URL.Query().Get("errors") == "true",
	}

	if minDur := r.URL.Query().Get("min_duration"); minDur != "" {
		if d, err := time.ParseDuration(minDur); err == nil {
			opts.MinDuration = d
		}
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	traces := s.traceBuffer.Filter(opts)
	if traces == nil {
		traces = []tracing.TraceRecord{}
	}
	writeJSON(w, traces)
}

// handleTraceDetail returns a single trace by ID.
func (s *Server) handleTraceDetail(w http.ResponseWriter, _ *http.Request, traceID string) {
	tr := s.traceBuffer.GetByID(traceID)
	if tr == nil {
		http.Error(w, "trace not found", http.StatusNotFound)
		return
	}
	writeJSON(w, tr)
}
