package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/catalog"
)

func TestRunSearch_JSONMergesCuratedAndRegistry(t *testing.T) {
	origRegistry := searchRegistry
	t.Cleanup(func() { searchRegistry = origRegistry })
	searchRegistry = func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return []catalog.Entry{{
			Name: "io.github.acme/github-alt", Description: "alt", Tier: catalog.TierRegistry,
			Install: catalog.Install{Type: catalog.InstallURL, Transport: "http", URL: "https://acme.com/mcp"},
		}}, false, nil
	}

	out := captureStdout(t, func() {
		if err := runSearch(context.Background(), "github", "json", "all", false); err != nil {
			t.Error(err)
		}
	})

	var doc searchDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, out)
	}
	if doc.SchemaVersion != searchJSONSchemaVersion || doc.Query != "github" {
		t.Errorf("doc header = %+v", doc)
	}
	var tiers []string
	for _, s := range doc.Servers {
		tiers = append(tiers, s.Tier)
	}
	if len(doc.Servers) < 2 || doc.Servers[0].Tier != catalog.TierCurated {
		t.Errorf("curated results should lead: %v", tiers)
	}
	if doc.Servers[len(doc.Servers)-1].Name != "io.github.acme/github-alt" {
		t.Errorf("registry result missing: %+v", doc.Servers)
	}
}

func TestRunSearch_RegistryFailureFallsBackToCurated(t *testing.T) {
	origRegistry := searchRegistry
	t.Cleanup(func() { searchRegistry = origRegistry })
	searchRegistry = func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		return nil, false, errors.New("connection refused")
	}

	out := captureStdout(t, func() {
		if err := runSearch(context.Background(), "github", "json", "all", false); err != nil {
			t.Errorf("registry failure must not fail search: %v", err)
		}
	})

	var doc searchDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.RegistryError == "" {
		t.Error("registry_error should be recorded")
	}
	if len(doc.Servers) == 0 {
		t.Error("curated results should still be returned")
	}
}

func TestRunSearch_EmptyQuerySkipsRegistry(t *testing.T) {
	origRegistry := searchRegistry
	t.Cleanup(func() { searchRegistry = origRegistry })
	called := false
	searchRegistry = func(ctx context.Context, query string) ([]catalog.Entry, bool, error) {
		called = true
		return nil, false, nil
	}

	_ = captureStdout(t, func() {
		if err := runSearch(context.Background(), "", "json", "all", false); err != nil {
			t.Error(err)
		}
	})
	if called {
		t.Error("empty query must not contact the registry")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 60); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
	long := strings.Repeat("x", 80)
	got := truncate(long, 60)
	if len([]rune(got)) != 60 || !strings.HasSuffix(got, "…") {
		t.Errorf("truncate long = %q (%d runes)", got, len([]rune(got)))
	}
}
