package catalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// rawCatalog is the embedded curated catalog. Entries are vetted by hand;
// see catalog_test.go for the invariants each entry must satisfy.
//
//go:embed data/catalog.json
var rawCatalog []byte

// parseCurated caches the embedded catalog parse. The error is retained so
// every caller sees a corrupt catalog rather than a silently empty one.
var parseCurated = sync.OnceValues(func() ([]Entry, error) {
	var entries []Entry
	if err := json.Unmarshal(rawCatalog, &entries); err != nil {
		return nil, fmt.Errorf("parsing embedded catalog: %w", err)
	}
	for i := range entries {
		entries[i].Tier = TierCurated
		if entries[i].Status == "" {
			entries[i].Status = StatusActive
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
})

// Curated returns the embedded curated entries, sorted by name. Callers
// must not mutate the returned slice.
func Curated() ([]Entry, error) {
	return parseCurated()
}

// FindCurated returns the curated entry with the given install name,
// matched case-insensitively.
func FindCurated(name string) (Entry, bool) {
	entries, err := parseCurated()
	if err != nil {
		return Entry{}, false
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) {
			return e, true
		}
	}
	return Entry{}, false
}

// FilterCurated returns the curated entries matching query as a
// case-insensitive substring of the name, title, or description. An empty
// query returns everything.
func FilterCurated(query string) ([]Entry, error) {
	entries, err := parseCurated()
	if err != nil {
		return nil, err
	}
	if query == "" {
		return entries, nil
	}
	q := strings.ToLower(query)
	var out []Entry
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), q) ||
			strings.Contains(strings.ToLower(e.Title), q) ||
			strings.Contains(strings.ToLower(e.Description), q) {
			out = append(out, e)
		}
	}
	return out, nil
}

// Merge combines curated and registry results: curated entries first, then
// registry entries sorted by name, dropping registry entries a curated
// entry already covers (matched by the curated entry's Namespace).
func Merge(curated, registry []Entry) []Entry {
	covered := make(map[string]bool, len(curated))
	for _, e := range curated {
		if e.Namespace != "" {
			covered[e.Namespace] = true
		}
	}
	merged := append([]Entry(nil), curated...)
	var rest []Entry
	for _, e := range registry {
		if !covered[e.Name] {
			rest = append(rest, e)
		}
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name })
	return append(merged, rest...)
}
