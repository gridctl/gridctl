package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/config"
)

func TestPrintPlanDiffSymbolsNoColorWhenPiped(t *testing.T) {
	diff := &config.PlanDiff{
		HasChanges: true,
		Summary:    "1 to add, 1 to change, 1 to destroy",
		Items: []config.DiffItem{
			{Action: config.DiffAdd, Kind: "mcp-server", Name: "foo"},
			{Action: config.DiffChange, Kind: "mcp-server", Name: "bar", Details: []string{"image: a -> b"}},
			{Action: config.DiffRemove, Kind: "mcp-server", Name: "baz"},
		},
	}

	var buf bytes.Buffer
	printPlanDiff(&buf, diff)
	out := buf.String()

	for _, want := range []string{`+ mcp-server "foo" (add)`, `~ mcp-server "bar" (update)`, `- mcp-server "baz" (destroy)`, "image: a -> b"} {
		if !strings.Contains(out, want) {
			t.Errorf("plan output missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "\033") {
		t.Errorf("plan output must be colorless on a non-TTY writer, got %q", out)
	}
}

func TestPrintPlanDiffNoChanges(t *testing.T) {
	var buf bytes.Buffer
	printPlanDiff(&buf, &config.PlanDiff{HasChanges: false})
	if !strings.Contains(buf.String(), "No changes") {
		t.Errorf("expected no-changes message, got %q", buf.String())
	}
}

func TestPrintPlanBuilds(t *testing.T) {
	builds := []config.BuildAction{{
		Server: "fetch", SourceType: "pypi", ImageTag: "gridctl-demo-fetch:0.6.0-a1b2c3d4",
		ResolvedIdentity: config.BuildIdentity{Package: "mcp-server-fetch", Version: "0.6.0"},
		CacheState:       "cached", GeneratedDockerfile: "FROM python\n",
	}}
	var buf bytes.Buffer
	printPlanBuilds(&buf, builds, true)
	out := buf.String()
	for _, want := range []string{"build pypi:mcp-server-fetch==0.6.0", "cache: cached", "FROM python"} {
		if !strings.Contains(out, want) {
			t.Errorf("build output missing %q in %q", want, out)
		}
	}
}

func TestResolvePlanBuilds_LocalDockerfileWithoutRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalRuntime := runtimeFlag
	runtimeFlag = "unavailable-test-runtime"
	t.Cleanup(func() { runtimeFlag = originalRuntime })

	stack := &config.Stack{Name: "demo", MCPServers: []config.MCPServer{{
		Name: "local", Source: &config.Source{Type: "local", Path: dir, Dockerfile: "Dockerfile"},
	}}}
	builds, err := resolvePlanBuilds(context.Background(), stack, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(builds) != 1 || builds[0].CacheState != "unknown" || builds[0].ImageTag == "" {
		t.Fatalf("builds = %+v", builds)
	}
}

func TestResolvePlanBuilds_PreservesFailedAction(t *testing.T) {
	originalRuntime := runtimeFlag
	runtimeFlag = "unavailable-test-runtime"
	t.Cleanup(func() { runtimeFlag = originalRuntime })

	stack := &config.Stack{Name: "demo", MCPServers: []config.MCPServer{{
		Name: "missing", Source: &config.Source{Type: "local", Path: filepath.Join(t.TempDir(), "missing"), Dockerfile: "Dockerfile"},
	}}}
	builds, err := resolvePlanBuilds(context.Background(), stack, false)
	if err == nil {
		t.Fatal("expected source resolution error")
	}
	if len(builds) != 1 || builds[0].DeclaredIdentity.Path == "" || builds[0].CacheState != "unknown" || builds[0].Error == "" {
		t.Fatalf("builds = %+v", builds)
	}
	data, marshalErr := json.Marshal(&config.PlanDiff{Builds: builds})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, want := range []string{`"declaredIdentity"`, `"cacheState":"unknown"`, `"error"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("plan JSON missing %s: %s", want, data)
		}
	}
}

func TestRunPlan_EncodesDiffWhenBuildResolutionFails(t *testing.T) {
	dir := t.TempDir()
	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	stackYAML := fmt.Sprintf(`name: plan-resolution-failure
mcp-servers:
  - name: broken
    transport: stdio
    source:
      type: local
      path: %q
      dockerfile: Missing.Dockerfile
`, dir)
	if err := os.WriteFile(stackPath, []byte(stackYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	originalFormat, originalRuntime := planFormat, runtimeFlag
	planFormat, runtimeFlag = "json", "unavailable-test-runtime"
	t.Cleanup(func() {
		planFormat, runtimeFlag = originalFormat, originalRuntime
	})

	var runErr error
	out := captureStdout(t, func() { runErr = runPlan(context.Background(), stackPath) })
	if runErr == nil {
		t.Fatal("expected source resolution error")
	}
	var diff config.PlanDiff
	if err := json.Unmarshal([]byte(out), &diff); err != nil {
		t.Fatalf("stdout is not a plan JSON document: %v\n%s", err, out)
	}
	if !diff.HasChanges || len(diff.Items) == 0 || len(diff.Builds) != 1 || diff.Builds[0].Error == "" {
		t.Fatalf("diff = %+v", diff)
	}
}

func TestPlanIdentitySanitizesURL(t *testing.T) {
	identity := planIdentity(builder.SourceIdentity{Type: "git", URL: "https://token@github.com/acme/repo.git?token=secret#fragment"})
	if identity.URL != "https://github.com/acme/repo.git" {
		t.Fatalf("URL = %q", identity.URL)
	}
}
