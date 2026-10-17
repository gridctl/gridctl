package api

import (
	"fmt"
	"net/http"
	"path"
	"strings"
)

// discoveryEntry is one enumerated public discovery document. The table is
// not configurable from the stack file.
type discoveryEntry struct {
	pattern string
	purpose string
	method  string
}

// builtinDiscoveryEntries is the final public discovery table: the gateway
// card, the group card, and the AI Catalog. Operational routes are rejected
// by newDiscoveryTable and are not listed here.
var builtinDiscoveryEntries = []discoveryEntry{
	{pattern: "/mcp/server-card", purpose: "SEP-2127 Server Card for this gateway", method: http.MethodGet},
	{pattern: "/groups/{name}/mcp/server-card", purpose: "SEP-2127 Server Card for one configured tool group", method: http.MethodGet},
	{pattern: "/.well-known/ai-catalog.json", purpose: "AI Catalog pointing at this gateway's Server Cards", method: http.MethodGet},
}

var (
	discoveryRoutes   []discoveryEntry
	discoveryTableErr error
)

func init() {
	discoveryRoutes, discoveryTableErr = newDiscoveryTable(builtinDiscoveryEntries)
}

// publicDiscoveryRoute reports whether path is an enumerated GET discovery
// document. Matching is exact on the cleaned path and allows one {name}
// segment. A failed table init matches nothing rather than panicking.
func publicDiscoveryRoute(raw string) bool {
	if discoveryTableErr != nil {
		return false
	}
	return discoveryMatch(discoveryRoutes, path.Clean(raw))
}

// discoveryRoutePatterns returns the table patterns for the security report.
func discoveryRoutePatterns() []string {
	if discoveryTableErr != nil {
		return nil
	}
	out := make([]string, len(discoveryRoutes))
	for i, entry := range discoveryRoutes {
		out[i] = entry.pattern
	}
	return out
}

var deniedDiscoveryPatterns = map[string]bool{
	"/mcp":               true,
	"/sse":               true,
	"/message":           true,
	"/groups/{name}/mcp": true,
	"/groups/{name}/sse": true,
}

// newDiscoveryTable rejects a table that would exempt an operational route
// or a non-GET method. Tests construct bad tables and expect an error.
func newDiscoveryTable(entries []discoveryEntry) ([]discoveryEntry, error) {
	out := make([]discoveryEntry, len(entries))
	copy(out, entries)
	for _, entry := range out {
		if entry.method != http.MethodGet {
			return nil, fmt.Errorf("discovery route %s: method %s is not GET", entry.pattern, entry.method)
		}
		if deniedDiscoveryPatterns[entry.pattern] {
			return nil, fmt.Errorf("discovery route %s is an operational route", entry.pattern)
		}
		if strings.HasPrefix(entry.pattern, "/api/") || strings.HasPrefix(entry.pattern, "/a2a/") {
			return nil, fmt.Errorf("discovery route %s is an operational prefix", entry.pattern)
		}
		if strings.Count(entry.pattern, "{") > 1 || strings.Count(entry.pattern, "}") > 1 {
			return nil, fmt.Errorf("discovery route %s has more than one parameter", entry.pattern)
		}
	}
	return out, nil
}

func discoveryMatch(entries []discoveryEntry, cleaned string) bool {
	for _, entry := range entries {
		if matchDiscoveryPattern(entry.pattern, cleaned) {
			return true
		}
	}
	return false
}

func matchDiscoveryPattern(pattern, cleaned string) bool {
	pp := strings.Split(pattern, "/")
	cp := strings.Split(cleaned, "/")
	if len(pp) != len(cp) {
		return false
	}
	for i := range pp {
		if pp[i] == "{name}" {
			if cp[i] == "" || strings.Contains(cp[i], "/") {
				return false
			}
			continue
		}
		if pp[i] != cp[i] {
			return false
		}
	}
	return true
}
