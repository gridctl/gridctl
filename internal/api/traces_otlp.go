package api

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/gridctl/gridctl/pkg/tracing"
)

// OTLP/JSON shapes per the OTLP specification encoding rules: lowerCamelCase
// keys, trace/span IDs as hex strings (the spec overrides protojson's
// base64-for-bytes mapping to match W3C Trace Context), and uint64 nanosecond
// timestamps as JSON strings. Hand-built rather than protojson.Marshal of
// tracepb structs precisely because protojson would emit base64 IDs.

type otlpAnyValue struct {
	StringValue string `json:"stringValue"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpEvent struct {
	TimeUnixNano string         `json:"timeUnixNano"`
	Name         string         `json:"name"`
	Attributes   []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpStatus struct {
	Code string `json:"code,omitempty"`
}

type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId,omitempty"`
	Name              string         `json:"name"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue `json:"attributes,omitempty"`
	Events            []otlpEvent    `json:"events,omitempty"`
	Status            otlpStatus     `json:"status"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpTracesData struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

// handleTraceOTLP serves GET /api/traces/{traceId}/otlp: the selected trace as
// a spec-shaped OTLP/JSON TracesData document, suitable for an OTel Collector
// file receiver or any OTLP JSON decoder.
func (s *Server) handleTraceOTLP(w http.ResponseWriter, r *http.Request) {
	if s.traceBuffer == nil {
		http.Error(w, "tracing disabled", http.StatusNotFound)
		return
	}
	traceID := r.PathValue("traceId")
	tr := s.traceBuffer.GetByID(traceID)
	if tr == nil {
		http.Error(w, "trace not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "trace-"+traceID+".json"))
	writeJSON(w, traceToOTLP(tr))
}

// traceToOTLP converts a buffered TraceRecord into the OTLP/JSON document shape.
func traceToOTLP(tr *tracing.TraceRecord) otlpTracesData {
	spans := make([]otlpSpan, len(tr.Spans))
	for i, sp := range tr.Spans {
		endTime := sp.EndTime
		if endTime.IsZero() {
			endTime = sp.StartTime
		}
		spans[i] = otlpSpan{
			TraceID:           sp.TraceID,
			SpanID:            sp.SpanID,
			ParentSpanID:      sp.ParentID,
			Name:              sp.Name,
			StartTimeUnixNano: fmt.Sprintf("%d", sp.StartTime.UnixNano()),
			EndTimeUnixNano:   fmt.Sprintf("%d", endTime.UnixNano()),
			Attributes:        attrsToOTLP(sp.Attrs),
			Status:            otlpStatus{Code: statusToOTLP(sp.Status)},
		}
		for _, ev := range sp.Events {
			spans[i].Events = append(spans[i].Events, otlpEvent{
				TimeUnixNano: fmt.Sprintf("%d", ev.Timestamp.UnixNano()),
				Name:         ev.Name,
				Attributes:   attrsToOTLP(ev.Attrs),
			})
		}
	}
	return otlpTracesData{
		ResourceSpans: []otlpResourceSpans{{
			Resource: otlpResource{
				Attributes: []otlpKeyValue{{
					Key:   "service.name",
					Value: otlpAnyValue{StringValue: "gridctl-gateway"},
				}},
			},
			ScopeSpans: []otlpScopeSpans{{
				Scope: otlpScope{Name: "gridctl.gateway"},
				Spans: spans,
			}},
		}},
	}
}

// attrsToOTLP converts a flat attribute map into sorted OTLP key/value pairs.
func attrsToOTLP(attrs map[string]string) []otlpKeyValue {
	if len(attrs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kvs := make([]otlpKeyValue, len(keys))
	for i, k := range keys {
		kvs[i] = otlpKeyValue{Key: k, Value: otlpAnyValue{StringValue: attrs[k]}}
	}
	return kvs
}

// statusToOTLP maps the buffer's status-code string (OTel codes.Code.String())
// to the OTLP JSON enum name. Unset maps to empty so the default is omitted.
func statusToOTLP(status string) string {
	switch status {
	case "Ok":
		return "STATUS_CODE_OK"
	case "Error":
		return "STATUS_CODE_ERROR"
	default:
		return ""
	}
}
