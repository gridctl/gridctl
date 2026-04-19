package probe

import (
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestCache_PutGetHit(t *testing.T) {
	c := NewCache(time.Minute)
	tools := []mcp.Tool{{Name: "search"}}
	c.Put("k1", tools)

	got, ok := c.Get("k1")
	if !ok {
		t.Fatalf("expected cache hit")
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "search" {
		t.Fatalf("tools mismatch: %+v", got.Tools)
	}
}

func TestCache_Miss(t *testing.T) {
	c := NewCache(time.Minute)
	if _, ok := c.Get("nope"); ok {
		t.Fatalf("expected miss")
	}
}

func TestCache_ExpiredEvictedOnAccess(t *testing.T) {
	c := NewCache(10 * time.Millisecond)
	base := time.Now()
	c.now = func() time.Time { return base }

	c.Put("k", []mcp.Tool{{Name: "t"}})
	if c.Len() != 1 {
		t.Fatalf("expected 1 entry after put, got %d", c.Len())
	}

	// Advance past TTL and read.
	c.now = func() time.Time { return base.Add(time.Second) }
	if _, ok := c.Get("k"); ok {
		t.Fatalf("expected miss for expired entry")
	}
	if c.Len() != 0 {
		t.Fatalf("expected lazy eviction, still have %d entries", c.Len())
	}
}

func TestCache_ZeroTTLUsesDefault(t *testing.T) {
	c := NewCache(0)
	if c.ttl != DefaultTTL {
		t.Fatalf("expected DefaultTTL, got %v", c.ttl)
	}
}

// Equivalent configs must produce the same key regardless of map iteration
// order — this is the core correctness property of canonicalization.
func TestKey_StableAcrossMapOrdering(t *testing.T) {
	a := config.MCPServer{
		Name:  "alpha",
		Image: "mcp/foo:latest",
		Env: map[string]string{
			"A": "1",
			"B": "2",
			"C": "3",
		},
		BuildArgs: map[string]string{
			"X": "10",
			"Y": "20",
		},
	}
	b := config.MCPServer{
		Name:  "beta", // different name should NOT change the key
		Image: "mcp/foo:latest",
		Env: map[string]string{
			"C": "3",
			"A": "1",
			"B": "2",
		},
		BuildArgs: map[string]string{
			"Y": "20",
			"X": "10",
		},
	}
	if Key(a) != Key(b) {
		t.Fatalf("keys differ for semantically equivalent configs: %q vs %q", Key(a), Key(b))
	}
}

func TestKey_DiffersOnMeaningfulChange(t *testing.T) {
	base := config.MCPServer{Image: "mcp/foo:latest", Env: map[string]string{"K": "v"}}
	changed := config.MCPServer{Image: "mcp/foo:latest", Env: map[string]string{"K": "v2"}}
	if Key(base) == Key(changed) {
		t.Fatalf("key did not change when env value changed")
	}

	urlCfg := config.MCPServer{URL: "https://example.com/mcp"}
	urlCfg2 := config.MCPServer{URL: "https://example.com/mcp/"}
	if Key(urlCfg) == Key(urlCfg2) {
		t.Fatalf("key did not differ for different URLs")
	}
}

func TestKey_IgnoresVolatileFields(t *testing.T) {
	a := config.MCPServer{Image: "mcp/foo", Name: "a", Network: "net-a"}
	b := config.MCPServer{Image: "mcp/foo", Name: "b", Network: "net-b"}
	if Key(a) != Key(b) {
		t.Fatalf("key changed for volatile fields (name/network): %q vs %q", Key(a), Key(b))
	}
}
