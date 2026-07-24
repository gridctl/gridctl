package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gridctl/gridctl/pkg/tracing"
)

// traceSummaryDTO maps a TraceRecord to the camelCase shape expected by the frontend.
type traceSummaryDTO struct {
	TraceID    string `json:"traceId"`
	RootSpanID string `json:"rootSpanId"`
	Operation  string `json:"operation"`
	Server     string `json:"server"`
	StartTime  string `json:"startTime"`
	Duration   int64  `json:"duration"`
	SpanCount  int    `json:"spanCount"`
	HasError   bool   `json:"hasError"`
	Status     string `json:"status"`
}

// traceListDTO is the envelope returned by GET /api/traces.
type traceListDTO struct {
	Traces []traceSummaryDTO `json:"traces"`
	Total  int               `json:"total"`
}

// spanEventDTO maps span events to the shape expected by the frontend.
type spanEventDTO struct {
	Name       string            `json:"name"`
	Timestamp  string            `json:"timestamp"`
	Attributes map[string]string `json:"attributes"`
}

// spanDTO maps a SpanRecord to the camelCase shape expected by the frontend.
type spanDTO struct {
	SpanID       string            `json:"spanId"`
	ParentSpanID string            `json:"parentSpanId"`
	Name         string            `json:"name"`
	StartTime    string            `json:"startTime"`
	EndTime      string            `json:"endTime,omitempty"`
	Duration     int64             `json:"duration"`
	Status       string            `json:"status"`
	Attributes   map[string]string `json:"attributes"`
	Events       []spanEventDTO    `json:"events"`
}

// traceDetailDTO is the envelope returned by GET /api/traces/{traceId}.
type traceDetailDTO struct {
	TraceID string    `json:"traceId"`
	Spans   []spanDTO `json:"spans"`
}

// handleTraces handles GET /api/traces and GET /api/traces/{traceId}.
func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	if s.traceBuffer == nil {
		writeJSON(w, traceListDTO{Traces: []traceSummaryDTO{}, Total: 0})
		return
	}

	if traceID := r.PathValue("traceId"); traceID != "" {
		s.handleTraceDetail(w, r, traceID)
		return
	}

	// List with optional filters.
	opts := tracing.FilterOpts{
		ServerName: r.URL.Query().Get("server"),
		ErrorsOnly: r.URL.Query().Get("errors") == "true",
	}

	// minDuration accepts a Go duration ("500ms", "2s") or a bare integer
	// interpreted as milliseconds. Unparseable input is a 400, not a silent
	// no-op: this is an operator-facing filter.
	if minDur := r.URL.Query().Get("minDuration"); minDur != "" {
		d, err := time.ParseDuration(minDur)
		if err != nil {
			if ms, aerr := strconv.Atoi(minDur); aerr == nil {
				d = time.Duration(ms) * time.Millisecond
			} else {
				http.Error(w, `invalid minDuration: use a Go duration (e.g. "500ms") or bare milliseconds`, http.StatusBadRequest)
				return
			}
		}
		if d < 0 {
			http.Error(w, "invalid minDuration: must not be negative", http.StatusBadRequest)
			return
		}
		opts.MinDuration = d
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil && n > 0 {
			opts.Limit = n
		}
	}
	if opts.Limit == 0 {
		opts.Limit = 100
	}

	records := s.traceBuffer.Filter(opts)
	summaries := make([]traceSummaryDTO, len(records))
	for i, tr := range records {
		status := "ok"
		if tr.IsError {
			status = "error"
		}
		rootSpanID := ""
		for _, sp := range tr.Spans {
			if sp.ParentID == "" {
				rootSpanID = sp.SpanID
				break
			}
		}
		summaries[i] = traceSummaryDTO{
			TraceID:    tr.TraceID,
			RootSpanID: rootSpanID,
			Operation:  tr.Operation,
			Server:     tr.ServerName,
			StartTime:  tr.StartTime.Format(time.RFC3339Nano),
			Duration:   tr.DurationMs,
			SpanCount:  tr.SpanCount,
			HasError:   tr.IsError,
			Status:     status,
		}
	}
	writeJSON(w, traceListDTO{Traces: summaries, Total: len(summaries)})
}

// handleTraceDetail returns a single trace by ID.
func (s *Server) handleTraceDetail(w http.ResponseWriter, _ *http.Request, traceID string) {
	tr := s.traceBuffer.GetByID(traceID)
	if tr == nil {
		http.Error(w, "trace not found", http.StatusNotFound)
		return
	}
	spans := make([]spanDTO, len(tr.Spans))
	for i, sp := range tr.Spans {
		status := "ok"
		if sp.IsError {
			status = "error"
		}
		attrs := sp.Attrs
		if attrs == nil {
			attrs = map[string]string{}
		}
		events := make([]spanEventDTO, len(sp.Events))
		for j, ev := range sp.Events {
			evAttrs := ev.Attrs
			if evAttrs == nil {
				evAttrs = map[string]string{}
			}
			events[j] = spanEventDTO{
				Name:       ev.Name,
				Timestamp:  ev.Timestamp.Format(time.RFC3339Nano),
				Attributes: evAttrs,
			}
		}
		endTime := ""
		if !sp.EndTime.IsZero() {
			endTime = sp.EndTime.Format(time.RFC3339Nano)
		}
		spans[i] = spanDTO{
			SpanID:       sp.SpanID,
			ParentSpanID: sp.ParentID,
			Name:         sp.Name,
			StartTime:    sp.StartTime.Format(time.RFC3339Nano),
			EndTime:      endTime,
			Duration:     sp.DurationMs,
			Status:       status,
			Attributes:   attrs,
			Events:       events,
		}
	}
	writeJSON(w, traceDetailDTO{TraceID: tr.TraceID, Spans: spans})
}
