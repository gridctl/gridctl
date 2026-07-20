package catalog

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

// defaultRegistryBaseURL is the official MCP Registry (API v0.1, frozen
// 2025-10-24). The registry's own guidance directs consumers to cache
// rather than query live; Client honors that with a TTL disk cache.
const defaultRegistryBaseURL = "https://registry.modelcontextprotocol.io"

const (
	registryRequestTimeout = 10 * time.Second
	registryCacheTTL       = time.Hour
	registryPageLimit      = 100
	// registryMaxPages bounds cursor-following per search; the CLI needs a
	// useful first screen, not a full mirror.
	registryMaxPages = 3
	// registryMaxBody caps a single response read (well above any real page).
	registryMaxBody = 8 << 20
)

// ErrNotFound reports a server name the MCP Registry does not know.
var ErrNotFound = errors.New("server not found in the MCP Registry")

// errHTTPNotFound is the raw 404 from any endpoint; only fetchServer folds
// it into ErrNotFound (a 404 on the list endpoint is an infrastructure
// failure, not a missing server).
var errHTTPNotFound = errors.New("MCP Registry returned 404 Not Found")

// Client queries the MCP Registry with an on-disk response cache. The zero
// value is not usable; construct with NewClient.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cacheDir   string
	ttl        time.Duration
	now        func() time.Time
}

// NewClient returns a registry client with the production base URL and the
// shared cache directory under ~/.gridctl/cache/catalog.
func NewClient() *Client {
	return &Client{
		baseURL:    defaultRegistryBaseURL,
		httpClient: &http.Client{Timeout: registryRequestTimeout},
		cacheDir:   filepath.Join(state.BaseDir(), "cache", "catalog"),
		ttl:        registryCacheTTL,
		now:        time.Now,
	}
}

// --- wire types: the subset of the registry's v0.1 API we consume ---

// serverResult is one registry entry: the server.json document plus the
// registry-managed metadata envelope.
type serverResult struct {
	Server registryServer `json:"server"`
	Meta   struct {
		Official *registryOfficial `json:"io.modelcontextprotocol.registry/official"`
	} `json:"_meta"`
}

type registryOfficial struct {
	Status   string `json:"status"`
	IsLatest bool   `json:"isLatest"`
}

type registryServer struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Title       string `json:"title"`
	Version     string `json:"version"`
	WebsiteURL  string `json:"websiteUrl"`
	Repository  *struct {
		URL string `json:"url"`
	} `json:"repository"`
	Packages []registryPackage   `json:"packages"`
	Remotes  []registryTransport `json:"remotes"`
}

type registryPackage struct {
	RegistryType         string             `json:"registryType"`
	Identifier           string             `json:"identifier"`
	Version              string             `json:"version"`
	RunTimeHint          string             `json:"runtimeHint"`
	Transport            registryTransport  `json:"transport"`
	PackageArguments     []registryArgument `json:"packageArguments"`
	EnvironmentVariables []registryKeyValue `json:"environmentVariables"`
}

type registryTransport struct {
	Type    string             `json:"type"`
	URL     string             `json:"url"`
	Headers []registryKeyValue `json:"headers"`
}

type registryInput struct {
	Description string   `json:"description"`
	IsRequired  bool     `json:"isRequired"`
	IsSecret    bool     `json:"isSecret"`
	Format      string   `json:"format"`
	Value       string   `json:"value"`
	Default     string   `json:"default"`
	Placeholder string   `json:"placeholder"`
	Choices     []string `json:"choices"`
}

type registryKeyValue struct {
	registryInput
	Name string `json:"name"`
}

type registryArgument struct {
	registryInput
	Type       string `json:"type"`
	Name       string `json:"name"`
	ValueHint  string `json:"valueHint"`
	IsRepeated bool   `json:"isRepeated"`
}

type registryListResponse struct {
	Servers  []serverResult `json:"servers"`
	Metadata struct {
		NextCursor string `json:"nextCursor"`
	} `json:"metadata"`
}

// --- public API ---

// Search queries /v0.1/servers with the substring query, following cursors
// up to a small page cap. Results convert to entries with deleted servers
// filtered out. stale reports that the returned entries came from an
// expired cache after a network failure; the error is non-nil only when
// neither the network nor any cache could serve the query.
func (c *Client) Search(ctx context.Context, query string) (entries []Entry, stale bool, err error) {
	cacheName := cacheFileName("search", query)
	if results, ok := c.readCache(cacheName, c.ttl); ok {
		return entriesFromResults(results), false, nil
	}

	results, fetchErr := c.fetchSearch(ctx, query)
	if fetchErr == nil {
		c.writeCache(cacheName, results)
		return entriesFromResults(results), false, nil
	}
	if results, ok := c.readCache(cacheName, 0); ok {
		slog.Debug("MCP Registry unreachable; serving stale cache", "error", fetchErr)
		return entriesFromResults(results), true, nil
	}
	return nil, false, fetchErr
}

// Get fetches the latest version of one server by its full registry name
// (e.g. "io.github.user/weather"). Returns ErrNotFound for unknown names
// and for deleted entries.
func (c *Client) Get(ctx context.Context, name string) (Entry, error) {
	cacheName := cacheFileName("server", name)
	results, ok := c.readCache(cacheName, c.ttl)
	if !ok {
		var fetchErr error
		results, fetchErr = c.fetchServer(ctx, name)
		if fetchErr == nil {
			c.writeCache(cacheName, results)
		} else if cached, hit := c.readCache(cacheName, 0); hit {
			slog.Debug("MCP Registry unreachable; serving stale cache", "error", fetchErr)
			results = cached
		} else {
			return Entry{}, fetchErr
		}
	}
	entries := entriesFromResults(results)
	if len(entries) == 0 {
		return Entry{}, ErrNotFound
	}
	return entries[0], nil
}

// entriesFromResults converts registry results to entries, dropping
// deleted servers.
func entriesFromResults(results []serverResult) []Entry {
	var entries []Entry
	for _, r := range results {
		if off := r.Meta.Official; off != nil && off.Status == "deleted" {
			continue
		}
		entries = append(entries, fromRegistry(r))
	}
	return entries
}

// --- fetching ---

func (c *Client) fetchSearch(ctx context.Context, query string) ([]serverResult, error) {
	var all []serverResult
	cursor := ""
	for page := 0; page < registryMaxPages; page++ {
		q := url.Values{}
		q.Set("version", "latest")
		q.Set("limit", fmt.Sprint(registryPageLimit))
		if query != "" {
			q.Set("search", query)
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var list registryListResponse
		if err := c.getJSON(ctx, "/v0.1/servers?"+q.Encode(), &list); err != nil {
			return nil, err
		}
		all = append(all, list.Servers...)
		cursor = list.Metadata.NextCursor
		if cursor == "" {
			break
		}
	}
	return all, nil
}

func (c *Client) fetchServer(ctx context.Context, name string) ([]serverResult, error) {
	var result serverResult
	path := "/v0.1/servers/" + url.PathEscape(name) + "/versions/latest"
	if err := c.getJSON(ctx, path, &result); err != nil {
		if errors.Is(err, errHTTPNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return []serverResult{result}, nil
}

func (c *Client) getJSON(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("contacting the MCP Registry: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return errHTTPNotFound
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("MCP Registry returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, registryMaxBody))
	if err != nil {
		return fmt.Errorf("reading MCP Registry response: %w", err)
	}
	if err := json.Unmarshal(body, into); err != nil {
		return fmt.Errorf("decoding MCP Registry response: %w", err)
	}
	return nil
}

// --- cache ---

type cacheDoc struct {
	FetchedAt time.Time      `json:"fetched_at"`
	Results   []serverResult `json:"results"`
}

// cacheFileName builds a stable, filesystem-safe cache file name for a
// kind ("search", "server") and key (query or server name).
func cacheFileName(kind, key string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(key)))
	return fmt.Sprintf("%s-%x.json", kind, sum[:8])
}

// readCache returns the cached results when the file exists and, for a
// non-zero maxAge, is younger than it. maxAge 0 accepts any age (the stale
// fallback path).
func (c *Client) readCache(name string, maxAge time.Duration) ([]serverResult, bool) {
	data, err := os.ReadFile(filepath.Join(c.cacheDir, name))
	if err != nil {
		return nil, false
	}
	var doc cacheDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, false
	}
	if maxAge > 0 && c.now().Sub(doc.FetchedAt) > maxAge {
		return nil, false
	}
	return doc.Results, true
}

// writeCache persists results best-effort; a failed write only costs the
// next call a refetch.
func (c *Client) writeCache(name string, results []serverResult) {
	doc := cacheDoc{FetchedAt: c.now(), Results: results}
	data, err := json.Marshal(doc)
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.cacheDir, 0o750); err != nil {
		slog.Debug("catalog cache directory unavailable", "dir", c.cacheDir, "error", err)
		return
	}
	if err := os.WriteFile(filepath.Join(c.cacheDir, name), data, 0o600); err != nil {
		slog.Debug("catalog cache write failed", "file", name, "error", err)
	}
}
