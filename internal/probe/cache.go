// Package probe implements the ephemeral MCP server probe used by the wizard
// to enumerate a server's tool list before it has been deployed.
//
// The cache keeps successful probe results for 5 minutes keyed by a
// canonicalized hash of the server config so repeated "Discover tools" clicks
// on the same config short-circuit the spawn.
package probe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// DefaultTTL is how long a successful probe result is cached.
const DefaultTTL = 5 * time.Minute

// Entry is a cached probe result.
type Entry struct {
	Tools    []mcp.Tool
	ProbedAt time.Time
}

// Cache is a concurrency-safe TTL cache for probe results. The zero value is
// not usable — call NewCache.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.RWMutex
	entries map[string]Entry
}

// NewCache constructs a probe cache with the given TTL. Zero or negative TTLs
// fall back to DefaultTTL so callers can't accidentally disable caching.
func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]Entry),
	}
}

// Get returns the entry for key if present and not expired. Expired entries
// are removed on access (lazy eviction).
func (c *Cache) Get(key string) (Entry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return Entry{}, false
	}
	if c.now().Sub(entry.ProbedAt) > c.ttl {
		c.mu.Lock()
		// Re-check under the write lock to avoid racing with a concurrent Put.
		if current, stillThere := c.entries[key]; stillThere && c.now().Sub(current.ProbedAt) > c.ttl {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return Entry{}, false
	}
	return entry, true
}

// Put stores an entry under the given key. Only callers that have a successful
// probe result should Put — failure paths must not populate the cache.
func (c *Cache) Put(key string, tools []mcp.Tool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = Entry{
		Tools:    tools,
		ProbedAt: c.now(),
	}
}

// Len returns the number of entries currently in the cache (including ones
// that are expired but not yet evicted). Intended for tests.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Key produces a stable sha256 hash of the subset of the server config that
// actually identifies the target being probed. Volatile / identity-only fields
// (name, network) are excluded so two identical servers with different names
// share a cache entry.
func Key(cfg config.MCPServer) string {
	canon := canonicalize(cfg)
	// json.Marshal sorts map keys for map types; we also sort our outer struct
	// by using a fixed field order below, so the output is stable across
	// semantically equivalent inputs.
	b, _ := json.Marshal(canon)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// canonicalShape is the shape the cache key hashes over. Field order here is
// the canonical order — do not reorder without also bumping a cache version
// (if one is introduced later) because that would invalidate existing keys.
type canonicalShape struct {
	Image         string              `json:"image,omitempty"`
	Source        *config.Source      `json:"source,omitempty"`
	URL           string              `json:"url,omitempty"`
	Port          int                 `json:"port,omitempty"`
	Transport     string              `json:"transport,omitempty"`
	Command       []string            `json:"command,omitempty"`
	Env           []kv                `json:"env,omitempty"`
	BuildArgs     []kv                `json:"build_args,omitempty"`
	SSH           *config.SSHConfig   `json:"ssh,omitempty"`
	OpenAPI       *config.OpenAPIConfig `json:"openapi,omitempty"`
	Replicas      int                 `json:"replicas,omitempty"`
	ReadyTimeout  string              `json:"ready_timeout,omitempty"`
}

// kv is a sorted representation of a map used inside canonicalShape. Using a
// slice of explicit pairs (sorted by key) guarantees the marshaled bytes are
// identical regardless of Go's map iteration order.
type kv struct {
	K string `json:"k"`
	V string `json:"v"`
}

func canonicalize(cfg config.MCPServer) canonicalShape {
	return canonicalShape{
		Image:        cfg.Image,
		Source:       cfg.Source,
		URL:          cfg.URL,
		Port:         cfg.Port,
		Transport:    cfg.Transport,
		Command:      cfg.Command,
		Env:          sortedKV(cfg.Env),
		BuildArgs:    sortedKV(cfg.BuildArgs),
		SSH:          cfg.SSH,
		OpenAPI:      cfg.OpenAPI,
		Replicas:     cfg.Replicas,
		ReadyTimeout: cfg.ReadyTimeout,
	}
}

func sortedKV(m map[string]string) []kv {
	if len(m) == 0 {
		return nil
	}
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{K: k, V: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].K < out[j].K })
	return out
}
