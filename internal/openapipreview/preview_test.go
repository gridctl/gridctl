package openapipreview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const petstoreSpec = `
openapi: 3.0.0
info:
  title: Petstore
  version: "1.0.17"
paths:
  /pets:
    get:
      operationId: listPets
      summary: List pets
      tags: [pet]
    post:
      summary: No operation id here
  /pets/{id}:
    delete:
      operationId: pets.delete
      summary: Dotted id
`

func specServer(t *testing.T, body, contentType string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPreview_ParsesAndReportsSkipped(t *testing.T) {
	srv := specServer(t, petstoreSpec, "application/yaml", http.StatusOK)
	p := New(NewCache(DefaultTTL), nil)

	result, prErr := p.Preview(context.Background(), Request{Spec: srv.URL})
	if prErr != nil {
		t.Fatalf("Preview: %s (%s)", prErr.Message, prErr.Code)
	}

	if result.Title != "Petstore" || result.Version != "1.0.17" {
		t.Errorf("identity = %q/%q, want Petstore/1.0.17", result.Title, result.Version)
	}
	if len(result.Operations) != 3 {
		t.Fatalf("expected 3 operations including skipped, got %d", len(result.Operations))
	}

	var skipped, dotted int
	for _, op := range result.Operations {
		if op.Skipped {
			skipped++
		}
		if op.OperationID == "pets.delete" {
			dotted++
			if op.ToolName != "pets_delete" {
				t.Errorf("ToolName = %q, want sanitized pets_delete", op.ToolName)
			}
		}
	}
	if skipped != 1 {
		t.Errorf("skipped count = %d, want 1", skipped)
	}
	if dotted != 1 {
		t.Error("dotted operationId not returned with its raw value")
	}
	if result.Cached {
		t.Error("first load should not be cached")
	}
}

func TestPreview_CachesSuccess(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(petstoreSpec))
	}))
	defer srv.Close()

	p := New(NewCache(DefaultTTL), nil)
	if _, err := p.Preview(context.Background(), Request{Spec: srv.URL}); err != nil {
		t.Fatalf("first Preview: %s", err.Message)
	}
	second, err := p.Preview(context.Background(), Request{Spec: srv.URL})
	if err != nil {
		t.Fatalf("second Preview: %s", err.Message)
	}
	if !second.Cached {
		t.Error("second load should report Cached")
	}
	if hits != 1 {
		t.Errorf("upstream fetched %d times, want 1", hits)
	}
}

func TestPreview_DoesNotCacheFailures(t *testing.T) {
	srv := specServer(t, "nope", "text/plain", http.StatusInternalServerError)
	cache := NewCache(DefaultTTL)
	p := New(cache, nil)

	if _, err := p.Preview(context.Background(), Request{Spec: srv.URL}); err == nil {
		t.Fatal("expected an error for a 500 response")
	}
	if cache.Len() != 0 {
		t.Errorf("cache holds %d entries after a failure, want 0", cache.Len())
	}
}

func TestPreview_ErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		ctype    string
		wantCode string
	}{
		{"unauthorized", http.StatusUnauthorized, "", "application/json", CodeNeedsAuth},
		{"forbidden", http.StatusForbidden, "", "application/json", CodeNeedsAuth},
		{"server error", http.StatusInternalServerError, "", "application/json", CodeFetchFailed},
		{"html docs page", http.StatusOK, "<html></html>", "text/html", CodeFetchFailed},
		{"not a spec", http.StatusOK, "just text", "application/json", CodeParseFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := specServer(t, tc.body, tc.ctype, tc.status)
			p := New(NewCache(DefaultTTL), nil)
			_, err := p.Preview(context.Background(), Request{Spec: srv.URL})
			if err == nil {
				t.Fatal("expected an error")
			}
			if err.Code != tc.wantCode {
				t.Errorf("code = %q, want %q (message: %s)", err.Code, tc.wantCode, err.Message)
			}
			if err.Hint == "" {
				t.Error("errors should carry an operator-facing hint")
			}
		})
	}
}

func TestPreview_EmptySpecRejected(t *testing.T) {
	p := New(NewCache(DefaultTTL), nil)
	_, err := p.Preview(context.Background(), Request{Spec: "   "})
	if err == nil {
		t.Fatal("expected an error for a blank spec")
	}
	if err.Code != CodeInvalidRequest {
		t.Errorf("code = %q, want %q", err.Code, CodeInvalidRequest)
	}
}

func TestPreview_LocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")
	if err := os.WriteFile(path, []byte(petstoreSpec), 0o600); err != nil {
		t.Fatalf("writing spec: %v", err)
	}

	p := New(NewCache(DefaultTTL), nil)
	result, err := p.Preview(context.Background(), Request{Spec: path})
	if err != nil {
		t.Fatalf("Preview: %s", err.Message)
	}
	if len(result.Operations) != 3 {
		t.Errorf("operations = %d, want 3", len(result.Operations))
	}
}

func TestPreview_MissingLocalFileHintsAtGatewayHost(t *testing.T) {
	p := New(NewCache(DefaultTTL), nil)
	_, err := p.Preview(context.Background(), Request{Spec: filepath.Join(t.TempDir(), "absent.yaml")})
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if err.Code != CodeFetchFailed {
		t.Errorf("code = %q, want %q", err.Code, CodeFetchFailed)
	}
}

// An unreachable host must classify as a fetch failure, not a parse failure.
// The two need different fixes, and telling an operator to check a document
// that was never served sends them at the wrong problem entirely.
func TestPreview_UnreachableHostIsFetchFailure(t *testing.T) {
	p := New(NewCache(DefaultTTL), nil)
	// .invalid is reserved by RFC 2606 and never resolves.
	_, err := p.Preview(context.Background(), Request{Spec: "https://gridctl-preview.invalid/openapi.json"})
	if err == nil {
		t.Fatal("expected an error for an unresolvable host")
	}
	if err.Code != CodeFetchFailed {
		t.Errorf("code = %q, want %q", err.Code, CodeFetchFailed)
	}
	if !strings.Contains(err.Hint, "reachable") {
		t.Errorf("hint = %q, want it to point at reachability", err.Hint)
	}
}

// A 401 and an unreachable host must stay distinguishable by code.
func TestPreview_AuthAndUnreachableUseDistinctCodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	p := New(NewCache(DefaultTTL), nil)
	_, authErr := p.Preview(context.Background(), Request{Spec: srv.URL})
	if authErr == nil || authErr.Code != CodeNeedsAuth {
		t.Fatalf("401 code = %v, want %q", authErr, CodeNeedsAuth)
	}

	_, unreachableErr := p.Preview(context.Background(), Request{Spec: "https://gridctl-preview.invalid/openapi.json"})
	if unreachableErr == nil || unreachableErr.Code == authErr.Code {
		t.Errorf("unreachable code = %v, want something other than %q", unreachableErr, authErr.Code)
	}
}

// An unexpanded variable is named as such rather than reported as whatever
// low-level failure the literal happens to produce.
func TestPreview_UnexpandedVariableIsNamed(t *testing.T) {
	p := New(NewCache(DefaultTTL), nil)
	for _, spec := range []string{"https://${API_HOST}/openapi.json", "/specs/${ENV}/openapi.yaml"} {
		_, err := p.Preview(context.Background(), Request{Spec: spec})
		if err == nil {
			t.Fatalf("spec %q: expected an error", spec)
		}
		if !strings.Contains(err.Hint, "unexpanded variable") {
			t.Errorf("spec %q: hint = %q, want it to name the unexpanded variable", spec, err.Hint)
		}
	}
}

func TestCache_ExpiresEntries(t *testing.T) {
	c := NewCache(time.Minute)
	now := time.Now()
	c.now = func() time.Time { return now }

	c.Put("k", Entry{Title: "T", LoadedAt: now})
	if _, ok := c.Get("k"); !ok {
		t.Fatal("entry should be present immediately after Put")
	}

	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("k"); ok {
		t.Error("entry should have expired")
	}
	if c.Len() != 0 {
		t.Errorf("expired entry not evicted, Len = %d", c.Len())
	}
}

func TestKey_StableAndDistinct(t *testing.T) {
	a := Request{Spec: "https://example.com/spec.json"}
	sameSpec := Request{Spec: "https://example.com/spec.json"}
	if Key(a) != Key(sameSpec) {
		t.Error("Key is not stable across separately constructed but identical requests")
	}
	b := Request{Spec: "https://example.com/other.json"}
	if Key(a) == Key(b) {
		t.Error("different specs must not share a cache key")
	}
	c := Request{Spec: a.Spec, InsecureSkipVerify: true}
	if Key(a) == Key(c) {
		t.Error("TLS verification mode must affect the cache key")
	}
}
