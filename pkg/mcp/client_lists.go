package mcp

import (
	"context"
	"encoding/json"
)

// maxListPages bounds a downstream cursor drain. Hitting it logs a warning
// and returns what was collected.
const maxListPages = 64

type listEnvelope struct {
	Resources         []json.RawMessage `json:"resources"`
	ResourceTemplates []json.RawMessage `json:"resourceTemplates"`
	Prompts           []json.RawMessage `json:"prompts"`
	NextCursor        string            `json:"nextCursor"`
	TTLMs             *int64            `json:"ttlMs"`
	CacheScope        string            `json:"cacheScope"`
}

func (e listEnvelope) field(key string) []json.RawMessage {
	switch key {
	case "resources":
		return e.Resources
	case "resourceTemplates":
		return e.ResourceTemplates
	default:
		return e.Prompts
	}
}

// ListResources drains resources/list. Handshake-era results report a nil
// TTL so the aggregate pins to uncacheable.
func (r *RPCClient) ListResources(ctx context.Context) (RawListPage, error) {
	return r.drainList(ctx, "resources/list", "resources")
}

// ListResourceTemplates drains resources/templates/list.
func (r *RPCClient) ListResourceTemplates(ctx context.Context) (RawListPage, error) {
	return r.drainList(ctx, "resources/templates/list", "resourceTemplates")
}

// ListPrompts drains prompts/list.
func (r *RPCClient) ListPrompts(ctx context.Context) (RawListPage, error) {
	return r.drainList(ctx, "prompts/list", "prompts")
}

func (r *RPCClient) drainList(ctx context.Context, method, key string) (RawListPage, error) {
	var entries []json.RawMessage
	cache := pageCache{allPublic: true}
	var cursor string
	for page := 0; page < maxListPages; page++ {
		var params any
		if cursor != "" {
			params = map[string]string{"cursor": cursor}
		}
		var env listEnvelope
		if err := r.transport.call(ctx, method, params, &env); err != nil {
			return RawListPage{}, err
		}
		entries = append(entries, env.field(key)...)
		cache.add(env.TTLMs, env.CacheScope, r.Era())
		if env.NextCursor == "" {
			out := cache.page()
			out.Entries = entries
			return out, nil
		}
		cursor = env.NextCursor
	}
	r.logger.Warn("downstream list page limit reached", "server", r.name, "method", method, "pages", maxListPages)
	out := cache.page()
	out.Entries = entries
	return out, nil
}

// pageCache folds list-level cache fields across a multi-page drain.
// A handshake-era server, or a page that omitted ttlMs, pins the page
// TTL to nil.
type pageCache struct {
	allPublic bool
	sawNil    bool
	hasTTL    bool
	min       int64
	saw       bool
}

func (c *pageCache) add(ttl *int64, scope string, era ProtocolEra) {
	c.saw = true
	if era != EraStateless || ttl == nil {
		c.sawNil = true
	} else if !c.hasTTL || *ttl < c.min {
		c.min = *ttl
		c.hasTTL = true
	}
	if scope != CacheScopePublic {
		c.allPublic = false
	}
}

func (c *pageCache) page() RawListPage {
	if !c.saw || c.sawNil || !c.hasTTL {
		return RawListPage{}
	}
	ttl := c.min
	scope := CacheScopePrivate
	if c.allPublic {
		scope = CacheScopePublic
	}
	return RawListPage{TTLMs: &ttl, CacheScope: scope}
}
