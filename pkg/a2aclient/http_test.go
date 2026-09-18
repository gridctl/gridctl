package a2aclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func randomSecret(t *testing.T) string {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b[:])
}

func TestClient_OriginAndSessions(t *testing.T) {
	token := randomSecret(t)
	var calls atomic.Int32
	var discovery string
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token || !validSession(r.Header.Get(sessionHeader)) || r.Header.Get(sessionHeader) == discovery {
			t.Error("RPC credential/session binding failed")
		}
		if r.Header.Get("A2A-Version") != "0.3" {
			t.Error("missing dialect header")
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32054,"message":"Session operation in progress, please retry"}}`)
	}))
	defer rpc.Close()
	card := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing discovery bearer")
		}
		discovery = r.Header.Get(sessionHeader)
		if !validSession(discovery) {
			t.Error("missing discovery UUID")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, rpc.URL, http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer card.Close()
	c, err := New(Options{Card: card.URL, Token: token, Bedrock: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.FetchCard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = c.ResolveEndpoint(rpc.URL); err == nil {
		t.Fatal("cross-origin advertisement accepted")
	}
	if _, _, err = c.RPC(t.Context(), rpc.URL, "0.3", discovery, nil); err == nil {
		t.Fatal("cross-origin RPC accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("unauthorized destination contacted")
	}
	c, err = New(Options{Card: card.URL + "/redirect", Endpoint: rpc.URL, Token: token, Bedrock: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.FetchCard(t.Context()); err == nil {
		t.Fatal("cross-origin redirect accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("redirect destination contacted")
	}
	endpoint, err := c.ResolveEndpoint(card.URL)
	if err != nil || endpoint != rpc.URL+"/" {
		t.Fatalf("explicit endpoint failed: %v", err)
	}
	// Use a separate client's randomly generated UUID as a conversation fixture.
	other, err := New(Options{Card: card.URL})
	if err != nil {
		t.Fatal(err)
	}
	result, status, err := c.RPC(t.Context(), endpoint, "0.3", other.discoverySession, []byte(`{}`))
	if err != nil || status != 409 || !strings.Contains(string(result), "-32054") {
		t.Fatalf("non-2xx body lost: status=%d err=%v", status, err)
	}
	if calls.Load() != 1 {
		t.Fatal("RPC retried")
	}
}

func TestClient_RedirectsBoundsAndGenericHeaders(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get(sessionHeader) != "" || r.Header.Get("Cookie") != "" {
			t.Error("ambient/session headers on generic request")
		}
		switch r.URL.Path {
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/large":
			_, _ = io.WriteString(w, strings.Repeat("x", maxRPCBytes+1))
		case "/rpc":
			http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer srv.Close()
	c, err := New(Options{Card: srv.URL + "/loop"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchCard(t.Context()); err == nil || calls.Load() != 4 {
		t.Fatal("card redirect bound not enforced")
	}
	c, err = New(Options{Card: srv.URL + "/large"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchCard(t.Context()); err == nil {
		t.Fatal("oversize card accepted")
	}
	if _, _, err := c.RPC(t.Context(), srv.URL+"/large", "1.0", "", nil); err == nil {
		t.Fatal("oversize RPC accepted")
	}
	before := calls.Load()
	if _, _, err := c.RPC(t.Context(), srv.URL+"/rpc", "1.0", "", nil); err == nil || calls.Load() != before+1 {
		t.Fatal("RPC redirect followed")
	}
	if _, _, err := c.RPC(t.Context(), srv.URL, "1.0.1", "", nil); err == nil {
		t.Fatal("patch version accepted")
	}
}

func TestClient_CancellationAndSafeErrors(t *testing.T) {
	c, err := New(Options{Card: "http://localhost"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = c.FetchCard(ctx)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "localhost") {
		t.Fatalf("unsafe or lost cancellation: %v", err)
	}
	_, _, err = c.RPC(ctx, "http://localhost", "1.0", "", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost RPC cancellation: %v", err)
	}
	for _, opts := range []Options{{Card: "file:///card"}, {Card: "https://example.com", Endpoint: "file:///rpc"}, {Card: "https://example.com", Token: "\n"}, {Card: "https://example.com", Timeout: -time.Second}} {
		if _, err := New(opts); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}

func TestClient_LoopbackIgnoresProxy(t *testing.T) {
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyCalls.Add(1); w.WriteHeader(502) }))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{}`) }))
	defer srv.Close()
	c, err := New(Options{Card: strings.Replace(srv.URL, "127.0.0.1", "localhost", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FetchCard(t.Context()); err != nil {
		t.Fatal(err)
	}
	if proxyCalls.Load() != 0 {
		t.Fatal("loopback request used environment proxy")
	}
}

func TestClient_OwnTimeoutPreservesIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/body" {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	for _, path := range []string{"/headers", "/body"} {
		c, err := New(Options{Card: srv.URL + path, Timeout: 20 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.FetchCard(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), srv.URL) {
			t.Fatalf("card timeout identity: %v", err)
		}
		_, _, err = c.RPC(t.Context(), srv.URL+path, "1.0", "", nil)
		if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), srv.URL) {
			t.Fatalf("RPC timeout identity: %v", err)
		}
	}
}

func TestClient_CardThrottlingCodes(t *testing.T) {
	for _, body := range []string{`busy`, `{"jsonrpc":"2.0","id":null,"error":{"code":-32053,"message":"private provider text"}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "4")
			w.WriteHeader(429)
			_, _ = io.WriteString(w, body)
		}))
		c, err := New(Options{Card: srv.URL})
		if err != nil {
			t.Fatal(err)
		}
		response, err := c.FetchCard(t.Context())
		srv.Close()
		var safe *Error
		if !errors.As(err, &safe) || safe.Status != 429 || !safe.Retryable || response.Header.Get("Retry-After") != "4" {
			t.Fatalf("throttle classification: %v", err)
		}
		if strings.HasPrefix(body, "{") && safe.Code != -32053 {
			t.Fatal("lost RPC code")
		}
		if strings.Contains(err.Error(), "private") || response.Body != nil {
			t.Fatal("retained remote failure text")
		}
	}
}
