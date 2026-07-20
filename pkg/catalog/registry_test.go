package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// newTestClient wires a Client at the test server with a temp cache dir and
// a controllable clock.
func newTestClient(t *testing.T, baseURL string) (*Client, *time.Time) {
	t.Helper()
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	c := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: time.Second},
		cacheDir:   t.TempDir(),
		ttl:        time.Hour,
		now:        func() time.Time { return now },
	}
	return c, &now
}

func listResponse(names ...string) registryListResponse {
	var list registryListResponse
	for _, name := range names {
		var r serverResult
		r.Server.Name = name
		r.Server.Description = "test"
		r.Server.Remotes = []registryTransport{{Type: "streamable-http", URL: "https://example.com/mcp"}}
		list.Servers = append(list.Servers, r)
	}
	return list
}

func TestSearch_FetchesAndCaches(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.URL.Query().Get("search"); got != "weather" {
			t.Errorf("search param = %q", got)
		}
		if got := r.URL.Query().Get("version"); got != "latest" {
			t.Errorf("version param = %q", got)
		}
		_ = json.NewEncoder(w).Encode(listResponse("io.github.a/weather"))
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL)
	entries, stale, err := c.Search(context.Background(), "weather")
	if err != nil || stale {
		t.Fatalf("err=%v stale=%v", err, stale)
	}
	if len(entries) != 1 || entries[0].Name != "io.github.a/weather" {
		t.Fatalf("entries = %+v", entries)
	}

	// Second call inside the TTL is served from cache.
	if _, _, err := c.Search(context.Background(), "weather"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("registry hit %d times; want 1 (cache)", calls)
	}
}

func TestSearch_FollowsCursors(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var list registryListResponse
		switch r.URL.Query().Get("cursor") {
		case "":
			list = listResponse("io.github.a/one")
			list.Metadata.NextCursor = "page2"
		case "page2":
			list = listResponse("io.github.a/two")
		default:
			t.Errorf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
		_ = json.NewEncoder(w).Encode(list)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL)
	entries, _, err := c.Search(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(entries) != 2 {
		t.Fatalf("calls=%d entries=%d; want 2 and 2", calls, len(entries))
	}
}

func TestSearch_StaleCacheFallback(t *testing.T) {
	healthy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(listResponse("io.github.a/weather"))
	}))
	defer srv.Close()

	c, now := newTestClient(t, srv.URL)
	if _, _, err := c.Search(context.Background(), "weather"); err != nil {
		t.Fatal(err)
	}

	// Expire the cache and break the network: stale results with the flag.
	*now = now.Add(2 * time.Hour)
	healthy = false
	entries, stale, err := c.Search(context.Background(), "weather")
	if err != nil {
		t.Fatalf("stale fallback should not error: %v", err)
	}
	if !stale || len(entries) != 1 {
		t.Fatalf("stale=%v entries=%d; want stale hit", stale, len(entries))
	}

	// No cache at all: the error surfaces.
	if _, _, err := c.Search(context.Background(), "other"); err == nil {
		t.Fatal("expected an error with no cache and no network")
	}
}

func TestGet_EncodesNameAndMapsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The slash in the server name must arrive percent-encoded.
		if r.URL.EscapedPath() != "/v0.1/servers/io.github.a%2Fweather/versions/latest" {
			http.NotFound(w, r)
			return
		}
		var result serverResult
		result.Server.Name = "io.github.a/weather"
		result.Server.Remotes = []registryTransport{{Type: "sse", URL: "https://example.com/sse"}}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL)
	entry, err := c.Get(context.Background(), "io.github.a/weather")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != "io.github.a/weather" || entry.Install.Transport != "sse" {
		t.Fatalf("entry = %+v", entry)
	}

	if _, err := c.Get(context.Background(), "io.github.a/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGet_DeletedIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var result serverResult
		result.Server.Name = "io.github.a/gone"
		result.Server.Remotes = []registryTransport{{Type: "sse", URL: "https://example.com/sse"}}
		result.Meta.Official = &registryOfficial{Status: "deleted"}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL)
	if _, err := c.Get(context.Background(), "io.github.a/gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound for deleted entries", err)
	}
}

func TestSearch_RespectsContextCancellation(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	c, _ := newTestClient(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := c.Search(ctx, "weather"); err == nil {
		t.Fatal("expected a context error")
	}
}

func TestCacheFileName_Stable(t *testing.T) {
	a := cacheFileName("search", "Weather")
	b := cacheFileName("search", "weather")
	if a != b {
		t.Errorf("cache key should be case-insensitive: %q vs %q", a, b)
	}
	if a == cacheFileName("search", "other") {
		t.Error("different queries must not collide")
	}
	// Names with path separators must not escape the cache dir.
	if got := cacheFileName("server", "io.github.a/../../etc"); url.PathEscape(got) != got {
		t.Errorf("cache file name %q is not filesystem-safe", got)
	}
}

func TestSearch_ListEndpoint404IsInfrastructure(t *testing.T) {
	// A 404 on the list endpoint is an outage, not a missing server: it must
	// not surface as ErrNotFound.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c, _ := newTestClient(t, srv.URL)
	_, _, err := c.Search(context.Background(), "weather")
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("list 404 must not map to ErrNotFound: %v", err)
	}
}
