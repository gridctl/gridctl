package mcp

import (
	"testing"
)

// cacheMetaClient builds a minimal downstream client with a resolved
// era and reported list cache metadata for aggregation tests.
func cacheMetaClient(name string, era ProtocolEra, ttlMs int64, scope string, known bool) *RPCClient {
	r := newFakeRPCClient(name, &fakeTransport{})
	r.SetEra(era)
	if known {
		r.SetListCacheMeta(ttlMs, scope)
	}
	return r
}

func TestAggregateListCacheMeta(t *testing.T) {
	tests := []struct {
		name      string
		clients   []*RPCClient
		wantTTL   int64
		wantScope string
	}{
		{
			name:      "empty fleet",
			wantTTL:   0,
			wantScope: CacheScopePrivate,
		},
		{
			name: "all modern public takes minimum",
			clients: []*RPCClient{
				cacheMetaClient("a", EraStateless, 5000, CacheScopePublic, true),
				cacheMetaClient("b", EraStateless, 3000, CacheScopePublic, true),
			},
			wantTTL:   3000,
			wantScope: CacheScopePublic,
		},
		{
			name: "one private narrows the scope",
			clients: []*RPCClient{
				cacheMetaClient("a", EraStateless, 5000, CacheScopePublic, true),
				cacheMetaClient("b", EraStateless, 3000, CacheScopePrivate, true),
			},
			wantTTL:   3000,
			wantScope: CacheScopePrivate,
		},
		{
			name: "any legacy server pins TTL to zero",
			clients: []*RPCClient{
				cacheMetaClient("a", EraStateless, 5000, CacheScopePublic, true),
				cacheMetaClient("b", EraHandshake, 0, "", false),
			},
			wantTTL:   0,
			wantScope: CacheScopePrivate,
		},
		{
			name: "modern server without observed meta is unknowable",
			clients: []*RPCClient{
				cacheMetaClient("a", EraStateless, 0, "", false),
			},
			wantTTL:   0,
			wantScope: CacheScopePrivate,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGateway()
			for _, c := range tc.clients {
				g.Router().AddClient(c)
			}
			ttl, scope := g.aggregateListCacheMeta()
			if ttl != tc.wantTTL || scope != tc.wantScope {
				t.Errorf("aggregate = %d/%s, want %d/%s", ttl, scope, tc.wantTTL, tc.wantScope)
			}
		})
	}
}

// TestAggregatedToolsOrderStable pins the deterministic merged-tool
// ordering the 2026-07-28 caching guidance depends on: same fleet, same
// order, every time.
func TestAggregatedToolsOrderStable(t *testing.T) {
	build := func() []Tool {
		g := NewGateway()
		zeta := newFakeRPCClient("zeta", &fakeTransport{})
		zeta.SetTools([]Tool{{Name: "z2"}, {Name: "z1"}})
		alpha := newFakeRPCClient("alpha", &fakeTransport{})
		alpha.SetTools([]Tool{{Name: "a1"}})
		// Registration order deliberately reversed vs. name order.
		g.Router().AddClient(zeta)
		g.Router().AddClient(alpha)
		return g.Router().AggregatedTools()
	}

	first := build()
	want := []string{"alpha__a1", "zeta__z2", "zeta__z1"}
	if len(first) != len(want) {
		t.Fatalf("expected %d tools, got %d", len(want), len(first))
	}
	for i, name := range want {
		if first[i].Name != name {
			t.Fatalf("order[%d] = %q, want %q (full: %v)", i, first[i].Name, name, first)
		}
	}
	for run := 0; run < 5; run++ {
		again := build()
		for i := range first {
			if again[i].Name != first[i].Name {
				t.Fatalf("ordering not stable across runs at index %d: %q vs %q", i, again[i].Name, first[i].Name)
			}
		}
	}
}
