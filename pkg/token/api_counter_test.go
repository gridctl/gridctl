package token

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPICounter_ImplementsInterface(t *testing.T) {
	c, err := NewAPICounter("test-key")
	if err != nil {
		t.Fatalf("NewAPICounter() error: %v", err)
	}
	// Compile-time check that APICounter satisfies the Counter interface.
	var _ Counter = c
}

func TestAPICounter_CountEmpty(t *testing.T) {
	c, err := NewAPICounter("test-key")
	if err != nil {
		t.Fatalf("NewAPICounter() error: %v", err)
	}
	// Empty string must return 0 without making any HTTP call.
	// (Server is not started — any HTTP call would panic or fail.)
	got := c.Count("")
	if got != 0 {
		t.Errorf("Count(\"\") = %d, want 0", got)
	}
}

func TestAPICounter_CountSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("missing or wrong x-api-key header")
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing anthropic-version header")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"input_tokens": 14}`)
	}))
	defer srv.Close()

	c, err := NewAPICounter("test-key")
	if err != nil {
		t.Fatalf("NewAPICounter() error: %v", err)
	}
	c = c.withEndpoint(srv.URL)

	got := c.Count("hello world")
	if got != 14 {
		t.Errorf("Count() = %d, want 14", got)
	}
}

func TestAPICounter_NonOKResponseFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c, err := NewAPICounter("test-key")
	if err != nil {
		t.Fatalf("NewAPICounter() error: %v", err)
	}
	c = c.withEndpoint(srv.URL)

	// Must not panic; must return a non-zero tiktoken fallback count.
	got := c.Count("hello world")
	if got <= 0 {
		t.Errorf("Count() = %d, want > 0 (tiktoken fallback)", got)
	}
}

func TestAPICounter_NetworkErrorFallsBack(t *testing.T) {
	// Start and immediately close a server to simulate a refused connection.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c, err := NewAPICounter("test-key")
	if err != nil {
		t.Fatalf("NewAPICounter() error: %v", err)
	}
	c = c.withEndpoint(srv.URL)

	// Must not panic; must return a non-zero tiktoken fallback count.
	got := c.Count("hello world")
	if got <= 0 {
		t.Errorf("Count() = %d, want > 0 (tiktoken fallback)", got)
	}
}

func TestAPICounter_MalformedResponseFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `not json at all`)
	}))
	defer srv.Close()

	c, err := NewAPICounter("test-key")
	if err != nil {
		t.Fatalf("NewAPICounter() error: %v", err)
	}
	c = c.withEndpoint(srv.URL)

	got := c.Count("hello world")
	if got <= 0 {
		t.Errorf("Count() = %d, want > 0 (tiktoken fallback)", got)
	}
}
