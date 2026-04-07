package mcp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fetchTestConfig returns a FetchConfig suitable for tests that hit local httptest servers.
// SSRF blocking is disabled to allow loopback connections; HTTPSOnly is disabled for plain HTTP.
func fetchTestConfig() FetchConfig {
	return FetchConfig{
		HTTPSOnly:            false,
		MaxResponseBytes:     DefaultFetchMaxResponseBytes,
		RequestTimeout:       DefaultFetchRequestTimeout,
		AllowPrivateNetworks: true,
	}
}

func TestSandbox_Fetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	sb := NewSandboxWithConfig(5*time.Second, fetchTestConfig())
	result, err := sb.Execute(t.Context(), fmt.Sprintf(`
		(async () => {
			const resp = await fetch(%q);
			const data = await resp.json();
			console.log(resp.ok ? "ok" : "not ok");
			console.log(String(resp.status));
			return data.hello;
		})()
	`, srv.URL), &mockToolCaller{}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value != `"world"` {
		t.Errorf("expected value %q, got %q", `"world"`, result.Value)
	}
	if len(result.Console) < 2 || result.Console[0] != "ok" || result.Console[1] != "200" {
		t.Errorf("unexpected console output: %v", result.Console)
	}
}

func TestSandbox_FetchSSRFBlocked_Loopback(t *testing.T) {
	// Start a real server on localhost so we have a valid port to target.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()

	// Use default config (AllowPrivateNetworks=false) — loopback must be blocked.
	sb := NewSandboxWithConfig(5*time.Second, FetchConfig{
		HTTPSOnly:        false,
		MaxResponseBytes: DefaultFetchMaxResponseBytes,
		RequestTimeout:   DefaultFetchRequestTimeout,
	})
	_, err := sb.Execute(t.Context(), fmt.Sprintf(`
		(async () => { await fetch(%q); })()
	`, srv.URL), &mockToolCaller{}, nil)

	if err == nil {
		t.Fatal("expected error for loopback fetch, got nil")
	}
	if !strings.Contains(err.Error(), "private network") {
		t.Errorf("expected 'private network' in error, got: %v", err)
	}
}

func TestSandbox_FetchSSRFBlocked_RFC1918(t *testing.T) {
	sb := NewSandboxWithConfig(5*time.Second, FetchConfig{
		HTTPSOnly:        false,
		MaxResponseBytes: DefaultFetchMaxResponseBytes,
		RequestTimeout:   DefaultFetchRequestTimeout,
	})
	_, err := sb.Execute(t.Context(), `
		(async () => { await fetch("http://192.168.1.1/secret"); })()
	`, &mockToolCaller{}, nil)

	if err == nil {
		t.Fatal("expected error for RFC 1918 fetch, got nil")
	}
	if !strings.Contains(err.Error(), "private network") {
		t.Errorf("expected 'private network' in error, got: %v", err)
	}
}

func TestSandbox_FetchHTTPSOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()

	// NewSandbox defaults to HTTPSOnly=true.
	sb := NewSandbox(5 * time.Second)
	_, err := sb.Execute(t.Context(), fmt.Sprintf(`
		(async () => { await fetch(%q); })()
	`, srv.URL), &mockToolCaller{}, nil)

	if err == nil {
		t.Fatal("expected error for http:// with HTTPSOnly=true, got nil")
	}
	if !strings.Contains(err.Error(), "only HTTPS") {
		t.Errorf("expected 'only HTTPS' in error, got: %v", err)
	}
}

func TestSandbox_FetchOversizedResponse(t *testing.T) {
	bigBody := strings.Repeat("x", 512)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bigBody))
	}))
	defer srv.Close()

	sb := NewSandboxWithConfig(5*time.Second, FetchConfig{
		HTTPSOnly:            false,
		MaxResponseBytes:     256, // tiny cap
		RequestTimeout:       DefaultFetchRequestTimeout,
		AllowPrivateNetworks: true,
	})
	_, err := sb.Execute(t.Context(), fmt.Sprintf(`
		(async () => { await fetch(%q); })()
	`, srv.URL), &mockToolCaller{}, nil)

	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
	if !strings.Contains(err.Error(), "exceeded") {
		t.Errorf("expected 'exceeded' in error, got: %v", err)
	}
}

func TestSandbox_FetchHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Test")
		w.Header().Set("X-Response", "present")
		_, _ = w.Write([]byte(`"done"`))
	}))
	defer srv.Close()

	sb := NewSandboxWithConfig(5*time.Second, fetchTestConfig())
	result, err := sb.Execute(t.Context(), fmt.Sprintf(`
		(async () => {
			const resp = await fetch(%q, {
				headers: { "X-Test": "hello" }
			});
			return resp.headers["x-response"];
		})()
	`, srv.URL), &mockToolCaller{}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "hello" {
		t.Errorf("request header not forwarded: got %q, want %q", gotHeader, "hello")
	}
	if result.Value != `"present"` {
		t.Errorf("response header not accessible: got %q, want %q", result.Value, `"present"`)
	}
}
