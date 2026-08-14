// Package openapipreview parses an OpenAPI spec on demand so the create-server
// wizard can show which operations exist before anything is deployed.
//
// It is deliberately a sibling of internal/probe rather than part of it. The
// probe performs an MCP handshake and returns []mcp.Tool, which discards the
// method, path, and tags a spec picker needs to filter on. The two share
// infrastructure patterns (TTL cache, stable error codes) but not a code path.
package openapipreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Stable error codes. The web UI keys its copy off these, so treat them as a
// wire contract: add new ones rather than repurposing existing ones.
const (
	CodeInvalidRequest = "invalid_request"
	CodeNeedsAuth      = "needs_auth"
	CodeFetchFailed    = "fetch_failed"
	CodeParseFailed    = "parse_failed"
	CodeRateLimited    = "rate_limited"
	CodeInternal       = "internal"
)

// DefaultTTL is how long a successful preview is cached.
const DefaultTTL = 5 * time.Minute

// Error is a structured preview failure carrying operator-facing guidance.
type Error struct {
	Code    string
	Message string
	Hint    string
}

func (e *Error) Error() string { return e.Message }

// Request identifies the spec to parse.
//
// There is deliberately no auth block: spec fetching is unauthenticated on the
// deployed path too, because API credentials authenticate calls to the API, not
// retrieval of its description. TLS material is accepted because a spec served
// from an mTLS-protected host cannot be fetched without it.
type Request struct {
	Spec               string
	CertFile           string
	KeyFile            string
	CAFile             string
	InsecureSkipVerify bool
}

// Result is a parsed spec's operation list plus the identity of the document.
type Result struct {
	Title      string
	Version    string
	Operations []mcp.OperationSummary
	Cached     bool
	LoadedAt   time.Time
}

// Previewer parses specs and caches successful results.
type Previewer struct {
	cache  *Cache
	logger *slog.Logger
}

// New constructs a Previewer. A nil cache disables caching; a nil logger falls
// back to the default.
func New(cache *Cache, logger *slog.Logger) *Previewer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Previewer{cache: cache, logger: logger}
}

// Preview loads a spec and returns its operations, including the ones that
// cannot become tools. Only successes are cached; failures always re-run so a
// transient outage does not pin an error for the cache lifetime.
func (p *Previewer) Preview(ctx context.Context, req Request) (*Result, *Error) {
	spec := strings.TrimSpace(req.Spec)
	if spec == "" {
		return nil, &Error{
			Code:    CodeInvalidRequest,
			Message: "No spec provided.",
			Hint:    "Enter an OpenAPI spec URL or a file path readable by the gateway.",
		}
	}
	req.Spec = spec

	key := Key(req)
	if p.cache != nil {
		if entry, ok := p.cache.Get(key); ok {
			return &Result{
				Title:      entry.Title,
				Version:    entry.Version,
				Operations: entry.Operations,
				Cached:     true,
				LoadedAt:   entry.LoadedAt,
			}, nil
		}
	}

	client, err := mcp.NewOpenAPIHTTPClient(req.CertFile, req.KeyFile, req.CAFile, req.InsecureSkipVerify)
	if err != nil {
		return nil, &Error{
			Code:    CodeInvalidRequest,
			Message: "TLS configuration is not usable: " + err.Error(),
			Hint:    "Check that the certificate, key, and CA file paths exist and are readable by the gateway.",
		}
	}

	doc, err := mcp.LoadOpenAPISpecForPreview(ctx, req.Spec, client)
	if err != nil {
		return nil, classifyLoadError(req.Spec, err)
	}

	operations := mcp.EnumerateOperations(doc)

	result := &Result{
		Operations: operations,
		LoadedAt:   time.Now().UTC(),
	}
	if doc.Info != nil {
		result.Title = doc.Info.Title
		result.Version = doc.Info.Version
	}

	if p.cache != nil {
		p.cache.Put(key, Entry{
			Title:      result.Title,
			Version:    result.Version,
			Operations: result.Operations,
			LoadedAt:   result.LoadedAt,
		})
	}

	p.logger.Debug("openapi preview parsed spec",
		"spec", req.Spec, "operations", len(operations))

	return result, nil
}

// classifyLoadError turns a load failure into operator-facing guidance. The
// distinction that matters most is auth: a 401 on the spec URL is a different
// problem from an unreachable host, and the fix is different too.
func classifyLoadError(spec string, err error) *Error {
	var fetchErr *mcp.SpecFetchError
	if errors.As(err, &fetchErr) {
		if fetchErr.StatusCode == http.StatusUnauthorized || fetchErr.StatusCode == http.StatusForbidden {
			return &Error{
				Code:    CodeNeedsAuth,
				Message: "The spec URL requires authentication.",
				Hint:    "Specs are fetched without credentials. Use a publicly reachable spec URL, a gateway-local file path, or enter operation IDs manually.",
			}
		}
		return &Error{
			Code:    CodeFetchFailed,
			Message: fetchErr.Error(),
			Hint:    "Check that the URL serves the OpenAPI document itself, not a documentation page.",
		}
	}

	msg := err.Error()

	// An unexpanded variable is checked before anything structural: it surfaces
	// as either a fetch or a read failure depending on whether the literal
	// looks like a URL, and both are baffling without naming the real cause.
	if strings.Contains(spec, "${") {
		return &Error{
			Code:    CodeParseFailed,
			Message: msg,
			Hint:    "The spec path still contains an unexpanded variable. Variables resolve at deploy time, so enter operation IDs manually or preview with a literal path.",
		}
	}

	// The host never answered: DNS failure, refused connection, TLS rejection,
	// or timeout. http.Client.Do returns *url.Error for all of them, and none
	// produce a SpecFetchError, which only wraps a non-2xx response. Without
	// this branch they fall through to "confirm the document is valid OpenAPI",
	// which sends the operator to inspect a document that was never served.
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		hint := "Confirm the URL is reachable from the gateway host. The gateway fetches the spec, not the browser, so a host resolvable only on your machine will fail here."
		if urlErr.Timeout() {
			hint = "The spec host did not respond in time. Confirm it is reachable from the gateway host, or enter operation IDs manually."
		}
		return &Error{Code: CodeFetchFailed, Message: msg, Hint: hint}
	}

	// Preview disables external $ref following, and kin-openapi enforces that by
	// failing the whole parse rather than omitting the referencing operations.
	// A multi-file spec, including one whose operations are all inline and only
	// its schemas are split out, therefore deploys fine but never previews. Say
	// so, instead of sending the operator to inspect a valid document.
	if strings.Contains(msg, "disallowed external reference") {
		return &Error{
			Code:    CodeParseFailed,
			Message: msg,
			Hint:    "This spec pulls in another document with $ref. Preview does not follow external references, so use a self-contained spec or enter operation IDs manually. Deploy is unaffected.",
		}
	}

	switch {
	case strings.Contains(msg, "reading spec file"):
		return &Error{
			Code:    CodeFetchFailed,
			Message: msg,
			Hint:    "Local spec paths are read by the gateway, not the browser. Confirm the path exists on the gateway host, or use a URL.",
		}
	case strings.Contains(msg, "content type"):
		return &Error{
			Code:    CodeFetchFailed,
			Message: msg,
			Hint:    "Point at the raw spec (often /openapi.json or /openapi.yaml) rather than the rendered docs page.",
		}
	default:
		return &Error{
			Code:    CodeParseFailed,
			Message: msg,
			Hint:    "Confirm the document is a valid OpenAPI 3.x spec.",
		}
	}
}

// Entry is a cached preview result.
type Entry struct {
	Title      string
	Version    string
	Operations []mcp.OperationSummary
	LoadedAt   time.Time
}

// Cache is a concurrency-safe TTL cache for preview results. The zero value is
// not usable — call NewCache.
type Cache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.RWMutex
	entries map[string]Entry
}

// NewCache constructs a preview cache with the given TTL. Zero or negative TTLs
// fall back to DefaultTTL so callers cannot accidentally disable caching.
func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Cache{ttl: ttl, now: time.Now, entries: make(map[string]Entry)}
}

// Get returns the entry for key if present and unexpired, evicting lazily.
func (c *Cache) Get(key string) (Entry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return Entry{}, false
	}
	if c.now().Sub(entry.LoadedAt) > c.ttl {
		c.mu.Lock()
		// Re-check under the write lock to avoid racing a concurrent Put.
		if current, stillThere := c.entries[key]; stillThere && c.now().Sub(current.LoadedAt) > c.ttl {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return Entry{}, false
	}
	return entry, true
}

// Put stores a successful preview. Failure paths must not populate the cache.
func (c *Cache) Put(key string, entry Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = entry
}

// Len reports the number of entries, including expired ones not yet evicted.
// Intended for tests.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Key produces a stable hash of everything that changes what a preview returns.
// Fields are hashed through a fixed-order struct so the bytes are stable across
// semantically identical requests.
func Key(req Request) string {
	shape := struct {
		Spec               string `json:"spec"`
		CertFile           string `json:"cert_file,omitempty"`
		KeyFile            string `json:"key_file,omitempty"`
		CAFile             string `json:"ca_file,omitempty"`
		InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
	}{
		Spec:               req.Spec,
		CertFile:           req.CertFile,
		KeyFile:            req.KeyFile,
		CAFile:             req.CAFile,
		InsecureSkipVerify: req.InsecureSkipVerify,
	}
	b, _ := json.Marshal(shape)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
