package tracing

import "testing"

func TestResolveOTLPEndpoint(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		signal string
		want   string
	}{
		{"pathless traces", "http://localhost:4318", SignalPathTraces, "http://localhost:4318/v1/traces"},
		{"slash traces", "http://localhost:4318/", SignalPathTraces, "http://localhost:4318/v1/traces"},
		{"pathless metrics", "http://localhost:4318", SignalPathMetrics, "http://localhost:4318/v1/metrics"},
		{"slash metrics", "http://localhost:4318/", SignalPathMetrics, "http://localhost:4318/v1/metrics"},
		{"explicit traces", "https://api.honeycomb.io/v1/traces", SignalPathTraces, "https://api.honeycomb.io/v1/traces"},
		{"explicit other", "https://example.test/otlp/v1/traces", SignalPathMetrics, "https://example.test/otlp/v1/traces"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveOTLPEndpoint(tt.raw, tt.signal)
			if err != nil {
				t.Fatalf("ResolveOTLPEndpoint: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveOTLPEndpoint_Rejects(t *testing.T) {
	if _, err := ResolveOTLPEndpoint("not a url", SignalPathTraces); err == nil {
		t.Fatal("expected error for a schemeless endpoint")
	}
	if _, err := ResolveOTLPEndpoint("", SignalPathTraces); err == nil {
		t.Fatal("expected error for an empty endpoint")
	}
}

func TestEndpointHost_OmitsUserinfo(t *testing.T) {
	if got := EndpointHost("https://user:secret@collector.example:4318/v1/traces"); got != "collector.example:4318" {
		t.Fatalf("host = %q", got)
	}
}
