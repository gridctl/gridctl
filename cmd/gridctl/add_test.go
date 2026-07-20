package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/catalog"
	"github.com/gridctl/gridctl/pkg/output"
)

// withAddFlags resets the add command's flag globals around a test.
func withAddFlags(t *testing.T) {
	t.Helper()
	origYes, origDryRun, origNoVault := addYes, addDryRun, addNoVault
	origFile, origName := addFile, addName
	origLookup := addRegistryLookup
	t.Cleanup(func() {
		addYes, addDryRun, addNoVault = origYes, origDryRun, origNoVault
		addFile, addName = origFile, origName
		addRegistryLookup = origLookup
	})
	addYes, addDryRun, addNoVault = false, false, false
	addFile, addName = "", ""
	addRegistryLookup = func(ctx context.Context, arg string) (*catalog.Entry, []catalog.Entry, error) {
		return nil, nil, catalog.ErrNotFound
	}
}

func writeTestStack(t *testing.T) string {
	t.Helper()
	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(stackPath, []byte(testStackYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return stackPath
}

func TestResolveCatalogEntry_CuratedAndSuggestion(t *testing.T) {
	withAddFlags(t)
	printer := output.NewWithWriter(os.Stderr)

	entry, err := resolveCatalogEntry(context.Background(), printer, "github")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Tier != catalog.TierCurated || entry.Name != "github" {
		t.Fatalf("entry = %+v", entry)
	}

	_, err = resolveCatalogEntry(context.Background(), printer, "gihub")
	if err == nil || !strings.Contains(err.Error(), `did you mean "github"`) {
		t.Fatalf("err = %v, want a github suggestion", err)
	}

	_, err = resolveCatalogEntry(context.Background(), printer, "definitely-not-a-server")
	if err == nil || !strings.Contains(err.Error(), "gridctl search") {
		t.Fatalf("err = %v, want a search hint", err)
	}
}

func TestResolveCatalogEntry_RegistryFallback(t *testing.T) {
	withAddFlags(t)
	want := catalog.Entry{
		Name: "io.github.acme/weather", Tier: catalog.TierRegistry,
		Install: catalog.Install{Type: catalog.InstallURL, Transport: "http", URL: "https://acme.com/mcp"},
	}
	addRegistryLookup = func(ctx context.Context, arg string) (*catalog.Entry, []catalog.Entry, error) {
		if arg != "io.github.acme/weather" {
			t.Errorf("lookup arg = %q", arg)
		}
		return &want, nil, nil
	}
	entry, err := resolveCatalogEntry(context.Background(), output.NewWithWriter(os.Stderr), "io.github.acme/weather")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Name != want.Name {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestRunAdd_DryRunWritesNothing(t *testing.T) {
	withAddFlags(t)
	stackPath := writeTestStack(t)
	addFile, addDryRun, addYes = stackPath, true, true

	out := captureStdout(t, func() {
		if err := runAdd(context.Background(), "fetch", "json"); err != nil {
			t.Error(err)
		}
	})

	after, _ := os.ReadFile(stackPath)
	if string(after) != testStackYAML {
		t.Error("dry run must not modify the stack file")
	}
	var doc addDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stdout is not a single JSON document: %v\n%s", err, out)
	}
	if !doc.DryRun || doc.Server.Added || doc.Server.Name != "fetch" {
		t.Errorf("doc = %+v", doc)
	}
}

func TestRunAdd_AppendsWithSecretPlaceholders(t *testing.T) {
	withAddFlags(t)
	stackPath := writeTestStack(t)
	addFile, addYes = stackPath, true

	out := captureStdout(t, func() {
		if err := runAdd(context.Background(), "github", "json"); err != nil {
			t.Error(err)
		}
	})

	text, err := os.ReadFile(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	stack := string(text)
	for _, want := range []string{
		"# my stack", "# keep this comment", // comments survive
		"name: github",
		"image: ghcr.io/github/github-mcp-server",
		"GITHUB_PERSONAL_ACCESS_TOKEN: ${var:GITHUB_PERSONAL_ACCESS_TOKEN}",
	} {
		if !strings.Contains(stack, want) {
			t.Errorf("stack missing %q:\n%s", want, stack)
		}
	}
	if backups, _ := filepath.Glob(stackPath + ".gridctl-backup-*"); len(backups) != 1 {
		t.Errorf("expected one backup, got %d", len(backups))
	}

	var doc addDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Server.Added || doc.BackupPath == "" {
		t.Errorf("doc = %+v", doc)
	}
	if len(doc.Server.UnsetVars) != 1 || doc.Server.UnsetVars[0] != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Errorf("unset_vars = %v", doc.Server.UnsetVars)
	}
}

func TestRunAdd_PositionalArgPlaceholder(t *testing.T) {
	withAddFlags(t)
	stackPath := writeTestStack(t)
	addFile, addYes = stackPath, true

	if err := runAdd(context.Background(), "filesystem", ""); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(stackPath)
	if !strings.Contains(string(text), "${var:ALLOWED_DIR}") {
		t.Errorf("required positional input should land as a placeholder:\n%s", text)
	}
}

func TestRunAdd_CollisionNonInteractiveFails(t *testing.T) {
	withAddFlags(t)
	stackPath := writeTestStack(t)
	addFile, addYes = stackPath, true
	// The fixture stack already has a server named "existing".
	addName = "existing"

	err := runAdd(context.Background(), "fetch", "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v, want a collision refusal", err)
	}
	after, _ := os.ReadFile(stackPath)
	if string(after) != testStackYAML {
		t.Error("collision must not modify the stack file")
	}
}

func TestRunAdd_UnknownNameFails(t *testing.T) {
	withAddFlags(t)
	stackPath := writeTestStack(t)
	addFile, addYes = stackPath, true

	if err := runAdd(context.Background(), "definitely-not-a-server", ""); err == nil {
		t.Fatal("expected an unknown-name error")
	}
}

func TestRouteInputValues(t *testing.T) {
	printer := output.NewWithWriter(os.Stderr)
	entry := catalog.Entry{Inputs: []catalog.Input{
		{Name: "API_KEY", Required: true, Secret: true},
		{Name: "REGION", Default: "us-east-1"},
		{Name: "OPTIONAL_KEY"},
	}}

	t.Run("unset required secret becomes a placeholder", func(t *testing.T) {
		withAddFlags(t)
		resolved, secrets, unset, err := routeInputValues(printer, entry, "svc", map[string]string{})
		if err != nil {
			t.Fatal(err)
		}
		if resolved["API_KEY"] != "${var:API_KEY}" {
			t.Errorf("resolved = %v", resolved)
		}
		if resolved["REGION"] != "us-east-1" {
			t.Errorf("default not applied: %v", resolved)
		}
		if _, ok := resolved["OPTIONAL_KEY"]; ok {
			t.Error("unset optional input should be omitted")
		}
		if len(secrets) != 0 || len(unset) != 1 || unset[0] != "API_KEY" {
			t.Errorf("secrets=%v unset=%v", secrets, unset)
		}
	})

	t.Run("no-vault keeps the literal with a warning", func(t *testing.T) {
		withAddFlags(t)
		addNoVault = true
		resolved, secrets, _, err := routeInputValues(printer, entry, "svc", map[string]string{"API_KEY": "sk-123"})
		if err != nil {
			t.Fatal(err)
		}
		if resolved["API_KEY"] != "sk-123" {
			t.Errorf("resolved = %v", resolved)
		}
		if len(secrets) != 1 || secrets[0].Action != "kept_literal" {
			t.Errorf("secrets = %v", secrets)
		}
	})

	t.Run("dry run reports vaulted without touching the store", func(t *testing.T) {
		withAddFlags(t)
		addDryRun = true
		resolved, secrets, _, err := routeInputValues(printer, entry, "svc", map[string]string{"API_KEY": "sk-123"})
		if err != nil {
			t.Fatal(err)
		}
		if resolved["API_KEY"] != "${var:API_KEY}" {
			t.Errorf("resolved = %v", resolved)
		}
		if len(secrets) != 1 || secrets[0].Action != "vaulted" {
			t.Errorf("secrets = %v", secrets)
		}
	})
}

func TestRunAdd_DeprecatedNonInteractiveNeedsYes(t *testing.T) {
	withAddFlags(t)
	stackPath := writeTestStack(t)
	addFile = stackPath
	deprecated := catalog.Entry{
		Name: "io.github.acme/old", Tier: catalog.TierRegistry, Status: catalog.StatusDeprecated,
		Install: catalog.Install{Type: catalog.InstallURL, Transport: "http", URL: "https://acme.com/mcp"},
	}
	addRegistryLookup = func(ctx context.Context, arg string) (*catalog.Entry, []catalog.Entry, error) {
		return &deprecated, nil, nil
	}

	err := runAdd(context.Background(), "io.github.acme/old", "")
	if err == nil || !strings.Contains(err.Error(), "deprecated") {
		t.Fatalf("err = %v, want a deprecated refusal", err)
	}

	// With --yes the warning stands but the add proceeds.
	addYes = true
	if err := runAdd(context.Background(), "io.github.acme/old", ""); err != nil {
		t.Fatalf("--yes should allow deprecated adds: %v", err)
	}
	text, _ := os.ReadFile(stackPath)
	if !strings.Contains(string(text), "https://acme.com/mcp") {
		t.Error("deprecated server was not appended under --yes")
	}
}

func TestAddDoc_JSONShape(t *testing.T) {
	doc := addDoc{
		SchemaVersion: addJSONSchemaVersion,
		StackFile:     "stack.yaml",
		Server: addServerDoc{
			Name: "github", CatalogName: "github", Tier: "curated", Added: true,
			Secrets: []importSecretDoc{{Key: "TOKEN", Action: "vaulted", Var: "TOKEN"}},
		},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"schema_version":1`, `"catalog_name":"github"`, `"added":true`, `"action":"vaulted"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("doc JSON missing %s: %s", want, data)
		}
	}
}

func TestUnsupportedEntryFailsAdd(t *testing.T) {
	withAddFlags(t)
	stackPath := writeTestStack(t)
	addFile, addYes = stackPath, true
	bundle := catalog.Entry{Name: "io.github.acme/bundle", Tier: catalog.TierRegistry, Unsupported: "mcpb"}
	addRegistryLookup = func(ctx context.Context, arg string) (*catalog.Entry, []catalog.Entry, error) {
		return &bundle, nil, nil
	}

	err := runAdd(context.Background(), "io.github.acme/bundle", "")
	var unsupported *catalog.UnsupportedInstallError
	if !errors.As(err, &unsupported) {
		t.Fatalf("err = %v, want UnsupportedInstallError", err)
	}
}

func TestResolveCatalogEntry_AmbiguousNameSurfaces(t *testing.T) {
	withAddFlags(t)
	addRegistryLookup = func(ctx context.Context, arg string) (*catalog.Entry, []catalog.Entry, error) {
		return nil, nil, &ambiguousNameError{arg: arg, names: []string{"io.github.a/pg", "io.github.b/pg"}}
	}
	_, err := resolveCatalogEntry(context.Background(), output.NewWithWriter(os.Stderr), "pg")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "io.github.a/pg") {
		t.Fatalf("err = %v, want the ambiguity guidance", err)
	}
}

func TestRouteInputValues_SanitizesVarKeys(t *testing.T) {
	withAddFlags(t)
	printer := output.NewWithWriter(os.Stderr)
	entry := catalog.Entry{Inputs: []catalog.Input{
		{Name: "api-key", Required: true, Secret: true},
	}}
	resolved, _, unset, err := routeInputValues(printer, entry, "svc", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	// The env key keeps the raw name; the reference must fit the ${var:KEY}
	// grammar (no dashes) or it can never resolve.
	if resolved["api-key"] != "${var:api_key}" {
		t.Errorf("resolved = %v", resolved)
	}
	if len(unset) != 1 || unset[0] != "api_key" {
		t.Errorf("unset = %v", unset)
	}
}
