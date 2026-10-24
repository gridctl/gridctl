package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/versions"
)

func TestCheckPreswap_Refusals(t *testing.T) {
	target := &versions.Schemas{DaemonState: 2, ProjectLock: 1, ImportLock: 8, ToolPins: "2", SkillPins: "1"}
	own := &versions.Schemas{DaemonState: 2, ProjectLock: 1, ImportLock: 8, ToolPins: "2", SkillPins: "1"}

	t.Run("daemon state", func(t *testing.T) {
		dir := preswapHome(t)
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":3,"stack_name":"netops","stack_file":"","pid":1,"auth_token":"secret-token"}`)
		got := mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0", Target: target, Own: own,
			StateDir: filepath.Join(dir, "state"),
		})
		assertHas(t, got.Refusals, "state/netops.json is schema version 3; v1.0.0 reads version 2")
		assertHas(t, got.Refusals, "this gridctl reads version 2")
		assertHas(t, got.Refusals, "gridctl destroy netops before rolling back")
		joined := strings.Join(append(got.Refusals, got.Warnings...), "\n")
		if strings.Contains(joined, "secret-token") || strings.Contains(joined, "auth_token") {
			t.Fatalf("refusal printed credential material:\n%s", joined)
		}
	})

	t.Run("daemon state above current only", func(t *testing.T) {
		dir := preswapHome(t)
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":3,"stack_name":"netops","pid":1}`)
		newer := *target
		newer.DaemonState = 4
		got := mustPreswap(t, preswapInput{
			TargetTag: "v2.0.0", Target: &newer, Own: own,
			StateDir: filepath.Join(dir, "state"),
		})
		if len(got.Refusals) != 1 || !strings.Contains(got.Refusals[0], "this gridctl reads version 2") {
			t.Fatalf("refusals = %v", got.Refusals)
		}
	})

	t.Run("tool pins", func(t *testing.T) {
		dir := preswapHome(t)
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":2,"stack_name":"netops","pid":1}`)
		pin := filepath.Join(dir, "pins", "netops.json")
		writeJSON(t, pin, `{"version":"3"}`)
		got := mustPreswap(t, pinInput(t, dir, target, own))
		assertHas(t, got.Refusals, pin+" is version 3; v1.0.0 reads version 2")
		assertHas(t, got.Refusals, "the daemon for netops will not start otherwise")
	})

	t.Run("skill pins", func(t *testing.T) {
		dir := preswapHome(t)
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":2,"stack_name":"netops","pid":1}`)
		pin := filepath.Join(dir, "pins", "skills", "netops.json")
		writeJSON(t, pin, `{"version":"2"}`)
		got := mustPreswap(t, pinInput(t, dir, target, own))
		assertHas(t, got.Refusals, pin+" is version 2; v1.0.0 reads version 1")
	})

	t.Run("project lock", func(t *testing.T) {
		dir := preswapHome(t)
		lock := filepath.Join(dir, "project.lock.yaml")
		writeJSON(t, lock, "version: 2\nprojections: []\n")
		got := mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0", Target: target, Own: own,
			ProjectLock: lock,
		})
		assertHas(t, got.Refusals, lock+" is version 2; v1.0.0 reads version 1")
		assertHas(t, got.Refusals, "~/.gridctl/project-migration-backup/")
	})

	t.Run("import lock", func(t *testing.T) {
		dir := preswapHome(t)
		lock := filepath.Join(dir, "skills.lock.yaml")
		writeJSON(t, lock, "version: 9\nsources:\n  pack:\n    pack:\n      name: netops\n")
		got := mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0", Target: target, Own: own,
			ImportLock: lock,
		})
		assertHas(t, got.Refusals, lock+" is version 9; v1.0.0 reads version 8")
		assertHas(t, got.Refusals, "gridctl pack remove netops")
	})

	t.Run("stack floor including parent-only", func(t *testing.T) {
		dir := preswapHome(t)
		parent := filepath.Join(dir, "parent.yaml")
		child := filepath.Join(dir, "child.yaml")
		writeJSON(t, parent, "name: parent\nrequires:\n  gridctl: \">=1.1.0\"\n")
		writeJSON(t, child, "name: child\nextends: parent.yaml\n")
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":2,"stack_name":"netops","stack_file":"`+child+`","pid":1}`)
		got := mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0", Target: target, Own: own,
			StateDir: filepath.Join(dir, "state"),
		})
		assertHas(t, got.Refusals, "stack netops ("+parent+") requires gridctl >=1.1.0; v1.0.0 does not satisfy it.")
	})

	t.Run("pack floor from lockfile", func(t *testing.T) {
		dir := preswapHome(t)
		lock := filepath.Join(dir, "skills.lock.yaml")
		writeJSON(t, lock, "version: 8\nsources:\n  pack:\n    pack:\n      name: netops\n      requires: \">=1.1.0\"\n")
		got := mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0", Target: target, Own: own,
			ImportLock: lock,
		})
		assertHas(t, got.Refusals, "pack netops requires gridctl >=1.1.0; v1.0.0 does not satisfy it.")
	})
}

func TestCheckPreswap_WarningsAndForce(t *testing.T) {
	target := &versions.Schemas{DaemonState: 2, ProjectLock: 1, ImportLock: 8, ToolPins: "2", SkillPins: "1"}
	own := target

	t.Run("unknown schemas", func(t *testing.T) {
		got := mustPreswap(t, preswapInput{TargetTag: "v0.1.0-rc.3"})
		if len(got.Refusals) != 0 {
			t.Fatalf("refusals = %v", got.Refusals)
		}
		assertHas(t, got.Warnings, unknownSchemasWarning("v0.1.0-rc.3"))
	})

	t.Run("unknown pin version", func(t *testing.T) {
		dir := preswapHome(t)
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":2,"stack_name":"netops","pid":1}`)
		writeJSON(t, filepath.Join(dir, "pins", "netops.json"), `{"version":"1"}`)
		blank := *target
		blank.ToolPins = ""
		got := mustPreswap(t, pinInput(t, dir, &blank, own))
		if len(got.Refusals) != 0 {
			t.Fatalf("refusals = %v", got.Refusals)
		}
		assertHas(t, got.Warnings, "warning: v1.0.0 does not advertise a tool pin version; pin compatibility for netops is unknown")
	})

	t.Run("unparsable pin is not zero", func(t *testing.T) {
		dir := preswapHome(t)
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":2,"stack_name":"netops","pid":1}`)
		writeJSON(t, filepath.Join(dir, "pins", "netops.json"), `{"version":"abc"}`)
		got := mustPreswap(t, pinInput(t, dir, target, own))
		if len(got.Refusals) != 0 {
			t.Fatalf("unparsable pin refused: %v", got.Refusals)
		}
		assertHas(t, got.Warnings, "which is not an integer")
	})

	t.Run("empty pin file version is compatible", func(t *testing.T) {
		dir := preswapHome(t)
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":2,"stack_name":"netops","pid":1}`)
		writeJSON(t, filepath.Join(dir, "pins", "netops.json"), `{"version":""}`)
		got := mustPreswap(t, pinInput(t, dir, target, own))
		if len(got.Refusals) != 0 || len(got.Warnings) != 0 {
			t.Fatalf("result = %+v", got)
		}
	})

	t.Run("missing stack file warns", func(t *testing.T) {
		dir := preswapHome(t)
		missing := filepath.Join(dir, "gone.yaml")
		writeJSON(t, filepath.Join(dir, "state", "netops.json"), `{"schema_version":2,"stack_name":"netops","stack_file":"`+missing+`","pid":1}`)
		got := mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0", Target: target, Own: own,
			StateDir: filepath.Join(dir, "state"),
		})
		if len(got.Refusals) != 0 {
			t.Fatalf("refusals = %v", got.Refusals)
		}
		assertHas(t, got.Warnings, "is missing")
	})

	t.Run("prerelease satisfies a range that includes it", func(t *testing.T) {
		dir := preswapHome(t)
		stack := filepath.Join(dir, "lab.yaml")
		writeJSON(t, stack, "name: lab\nrequires:\n  gridctl: \">=0.1.0\"\n")
		writeJSON(t, filepath.Join(dir, "state", "lab.json"), `{"schema_version":2,"stack_name":"lab","stack_file":"`+stack+`","pid":1}`)
		got := mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0-rc.2", Target: target, Own: own,
			StateDir: filepath.Join(dir, "state"),
		})
		if len(got.Refusals) != 0 {
			t.Fatalf("rc should satisfy >=0.1.0: %v", got.Refusals)
		}
		writeJSON(t, stack, "name: lab\nrequires:\n  gridctl: \">=1.0.0\"\n")
		got = mustPreswap(t, preswapInput{
			TargetTag: "v1.0.0-rc.2", Target: target, Own: own,
			StateDir: filepath.Join(dir, "state"),
		})
		assertHas(t, got.Refusals, "does not satisfy it")
	})

	t.Run("force downgrades refusals", func(t *testing.T) {
		result := preswapResult{Refusals: []string{"project lock is newer"}, Warnings: []string{"warning: pin compatibility is unknown"}}
		proceed, lines := rollbackRefused("v1.0.0", result, true)
		if !proceed {
			t.Fatal("force did not proceed")
		}
		text := strings.Join(lines, "\n")
		if strings.Contains(text, "Rollback to") || strings.Contains(text, "Pass --force") {
			t.Fatalf("force printed a refusal block:\n%s", text)
		}
		if !strings.Contains(text, "warning: project lock is newer") || !strings.Contains(text, "warning: pin compatibility is unknown") {
			t.Fatalf("lines = %v", lines)
		}
		proceed, lines = rollbackRefused("v1.0.0", result, false)
		if proceed {
			t.Fatal("refusal proceeded")
		}
		text = strings.Join(lines, "\n")
		if !strings.Contains(text, "Rollback to v1.0.0 refused:") || !strings.Contains(text, "Pass --force to roll back anyway.") {
			t.Fatalf("lines = %v", lines)
		}
	})
}

func preswapHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"state", "pins", "pins/skills"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func pinInput(t *testing.T, dir string, target, own *versions.Schemas) preswapInput {
	t.Helper()
	return preswapInput{
		TargetTag: "v1.0.0",
		Target:    target,
		Own:       own,
		StateDir:  filepath.Join(dir, "state"),
		ToolPin:   func(name string) (string, error) { return filepath.Join(dir, "pins", name+".json"), nil },
		SkillPin:  func(name string) (string, error) { return filepath.Join(dir, "pins", "skills", name+".json"), nil },
	}
}

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustPreswap(t *testing.T, in preswapInput) preswapResult {
	t.Helper()
	got, err := checkPreswap(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertHas(t *testing.T, lines []string, want string) {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, want) {
			return
		}
	}
	t.Fatalf("missing %q in:\n%s", want, strings.Join(lines, "\n"))
}
