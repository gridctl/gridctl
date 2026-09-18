package a2aclient

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCardCache_FreshnessAndApproval(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Date", "")
		if fail.Load() {
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"protocolVersion": "0.3.0", "url": "http://" + r.Host})
	}))
	defer srv.Close()
	client, err := New(Options{Card: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCardCache(client, "auto")
	now := time.Now()
	cache.now = func() time.Time { return now }
	body, err := cache.Fetch(t.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	body[0] = 'x'
	for range 3 {
		body, err := cache.Fetch(t.Context(), false)
		if err != nil || body[0] != '{' {
			t.Fatal("cache evidence mutated")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("cache hit performed GET")
	}
	if _, err := cache.Fetch(t.Context(), true); err != nil || calls.Load() != 2 {
		t.Fatal("approval reused cache")
	}
	now = now.Add(30 * time.Second)
	fail.Store(true)
	if _, err := cache.Fetch(t.Context(), false); err == nil {
		t.Fatal("stale served on error")
	}
	for _, force := range []bool{false, true} {
		if _, err := cache.Fetch(t.Context(), force); err == nil {
			t.Fatal("backoff served stale")
		}
	}
	if calls.Load() != 3 {
		t.Fatal("backoff failed")
	}
	now = now.Add(10 * time.Second)
	fail.Store(false)
	if _, err := cache.Fetch(t.Context(), false); err != nil || calls.Load() != 4 {
		t.Fatal("backoff did not expire")
	}
}

func TestCardCache_CoalescingAndWaiterCancellation(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"protocolVersion": "0.3.0", "url": "http://" + r.Host})
	}))
	defer srv.Close()
	client, err := New(Options{Card: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCardCache(client, "auto")
	var wg sync.WaitGroup
	wg.Go(func() {
		if _, err := cache.Fetch(t.Context(), false); err != nil {
			t.Error(err)
		}
	})
	<-started
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := cache.Fetch(ctx, false); !errors.Is(err, context.Canceled) {
		t.Error("waiter cancellation lost")
	}
	for range 20 {
		wg.Go(func() {
			if _, err := cache.Fetch(t.Context(), false); err != nil {
				t.Error(err)
			}
		})
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("refresh not coalesced")
	}
}

func TestCardCache_NoStoreAndParseFailure(t *testing.T) {
	var calls atomic.Int32
	var malformed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		if malformed.Load() {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"protocolVersion": "0.3.0", "url": "http://" + r.Host})
	}))
	defer srv.Close()
	client, err := New(Options{Card: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCardCache(client, "auto")
	for range 2 {
		if _, err := cache.Fetch(t.Context(), false); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 || cache.body != nil {
		t.Fatal("no-store response reused")
	}
	malformed.Store(true)
	for range 2 {
		if _, err := cache.Fetch(t.Context(), false); err == nil {
			t.Fatal("malformed card accepted")
		}
	}
	if calls.Load() != 3 {
		t.Fatal("parse failure not coalesced")
	}
}

func TestFreshnessLifetime(t *testing.T) {
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		control, age string
		want         time.Duration
	}{
		{"", "", 30 * time.Second}, {"max-age=120", "", 30 * time.Second},
		{"max-age=5", "", 5 * time.Second}, {"no-cache", "", 0}, {"no-store", "", 0},
		{"max-age=0", "", 0}, {"max-age=10", "7", 3 * time.Second}, {"max-age=invalid", "", 0},
	} {
		h := http.Header{"Cache-Control": {tt.control}, "Age": {tt.age}}
		if got := freshnessLifetime(h, now); got != tt.want {
			t.Errorf("%s age=%s: %s != %s", tt.control, tt.age, got, tt.want)
		}
	}
	h := http.Header{"Date": {now.Add(-10 * time.Second).Format(http.TimeFormat)}}
	if got := freshnessLifetime(h, now); got != 20*time.Second {
		t.Fatalf("Date ignored: %s", got)
	}
	h.Set("Expires", now.Add(5*time.Second).Format(http.TimeFormat))
	if got := freshnessLifetime(h, now); got != 5*time.Second {
		t.Fatalf("Expires ignored: %s", got)
	}
	if got := failureDelay(http.Header{"Retry-After": {"999"}}, now); got != 30*time.Second {
		t.Fatal("unbounded backoff")
	}
}

func TestCardCache_ByteChangeAndShortLifetimes(t *testing.T) {
	for _, control := range []string{"max-age=2", "no-cache", "no-store", "max-age=0"} {
		t.Run(control, func(t *testing.T) {
			var changed atomic.Bool
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Date", "")
				w.Header().Set("Cache-Control", control)
				if changed.Load() {
					_, _ = io.WriteString(w, " ")
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"protocolVersion": "0.3.0", "url": "http://" + r.Host})
			}))
			defer srv.Close()
			client, err := New(Options{Card: srv.URL})
			if err != nil {
				t.Fatal(err)
			}
			cache := NewCardCache(client, "auto")
			now := time.Now()
			cache.now = func() time.Time { return now }
			before, err := cache.Fetch(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			changed.Store(true)
			if control == "max-age=2" {
				now = now.Add(time.Second)
				cached, err := cache.Fetch(t.Context(), false)
				if err != nil || string(cached) != string(before) || calls.Load() != 1 {
					t.Fatal("short freshness not reused")
				}
				now = now.Add(time.Second)
			}
			after, err := cache.Fetch(t.Context(), false)
			if err != nil || string(after) == string(before) || calls.Load() != 2 {
				t.Fatal("byte-only change hidden after expiry")
			}
		})
	}
}

func TestCardCache_CoalescesFailures(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(429)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","error":{"code":-32053,"message":"busy"}}`)
	}))
	defer srv.Close()
	client, err := New(Options{Card: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	cache := NewCardCache(client, "auto")
	now := time.Now()
	cache.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			body, err := cache.Fetch(t.Context(), false)
			var safe *Error
			if !errors.As(err, &safe) || safe.Code != -32053 || body != nil {
				t.Error("failed refresh served evidence")
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("failed refresh not coalesced")
	}
	now = now.Add(time.Second)
	if _, err := cache.Fetch(t.Context(), true); err == nil || calls.Load() != 2 {
		t.Fatal("one-second backoff did not expire")
	}
}
