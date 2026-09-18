package a2aclient

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CardCache is private to one registration and credential/session identity.
// Approved trust must be stored separately; a cache hit is not approval.
type CardCache struct {
	mu      sync.Mutex
	client  *Client
	dialect string
	now     func() time.Time
	until   time.Time
	backoff time.Time
	body    []byte
	failure error
	flight  *cardFlight
}

type cardFlight struct {
	done chan struct{}
	body []byte
	err  error
}

// NewCardCache binds freshness to an immutable client and configured dialect.
func NewCardCache(client *Client, dialect string) *CardCache {
	return &CardCache{client: client, dialect: dialect, now: time.Now}
}

// Fetch returns validated card bytes. Force bypasses successful cache reuse for
// approval, but still honors bounded failure backoff. Each caller owns its bytes.
// Admission/capability checks must precede this call.
func (c *CardCache) Fetch(ctx context.Context, force bool) ([]byte, error) {
	for {
		if ctx.Err() != nil {
			return nil, safeError(ctx, "card_refresh_canceled")
		}
		c.mu.Lock()
		now := c.now()
		if now.Before(c.backoff) {
			err := c.failure
			c.mu.Unlock()
			return nil, err
		}
		if !force && now.Before(c.until) {
			body := append([]byte(nil), c.body...)
			c.mu.Unlock()
			return body, nil
		}
		if flight := c.flight; flight != nil {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, safeError(ctx, "card_refresh_canceled")
			case <-flight.done:
				if force {
					continue
				}
				return append([]byte(nil), flight.body...), flight.err
			}
		}
		flight := &cardFlight{done: make(chan struct{})}
		c.flight = flight
		c.mu.Unlock()

		refreshCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var response CardResponse
		var err error
		if c.client == nil {
			err = &Error{Category: "card_client_unavailable"}
		} else {
			response, err = c.client.FetchCard(refreshCtx)
			if err == nil {
				var iface Interface
				_, iface, err = ParseCard(response.Body, c.dialect)
				if err == nil {
					_, err = c.client.ResolveEndpoint(iface.URL)
				}
			}
		}
		cancel()
		c.mu.Lock()
		c.until, c.body = time.Time{}, nil
		if err != nil {
			c.failure = err
			c.backoff = c.now().Add(failureDelay(response.Header, c.now()))
			flight.err = err
		} else {
			c.failure, c.backoff = nil, time.Time{}
			flight.body = append([]byte(nil), response.Body...)
			lifetime := freshnessLifetime(response.Header, now)
			if lifetime > 0 {
				c.until = now.Add(lifetime)
				c.body = append([]byte(nil), response.Body...)
			}
		}
		c.flight = nil
		close(flight.done)
		c.mu.Unlock()
		return append([]byte(nil), flight.body...), flight.err
	}
}

func freshnessLifetime(h http.Header, now time.Time) time.Duration {
	lifetime := 30 * time.Second
	for _, value := range h.Values("Cache-Control") {
		for _, directive := range strings.Split(value, ",") {
			key, value, _ := strings.Cut(strings.TrimSpace(directive), "=")
			switch strings.ToLower(key) {
			case "no-cache", "no-store":
				return 0
			case "max-age":
				n, err := strconv.ParseUint(strings.Trim(value, `"`), 10, 31)
				if err != nil {
					return 0
				}
				lifetime = min(lifetime, time.Duration(n)*time.Second)
			}
		}
	}
	date, dateErr := http.ParseTime(h.Get("Date"))
	age := time.Duration(0)
	if dateErr == nil && now.After(date) {
		age = now.Sub(date)
	}
	if value := h.Get("Age"); value != "" {
		n, err := strconv.ParseUint(value, 10, 31)
		if err != nil {
			return 0
		}
		age = max(age, time.Duration(n)*time.Second)
	}
	if value := h.Get("Expires"); value != "" {
		expires, err := http.ParseTime(value)
		if err != nil {
			return 0
		}
		base := now
		if dateErr == nil {
			base = date
		}
		lifetime = min(lifetime, expires.Sub(base))
	}
	return max(0, lifetime-age)
}

func failureDelay(h http.Header, now time.Time) time.Duration {
	value := h.Get("Retry-After")
	if n, err := strconv.ParseUint(value, 10, 31); err == nil {
		return max(time.Second, min(30*time.Second, time.Duration(n)*time.Second))
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(time.Second, min(30*time.Second, date.Sub(now)))
	}
	return time.Second
}
