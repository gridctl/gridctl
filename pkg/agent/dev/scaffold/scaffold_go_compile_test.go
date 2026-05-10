package scaffold

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestScaffoldGoOutputCompiles runs the Go scaffold output through
// `go build` and `go test` to confirm a freshly-scaffolded Go skill
// is buildable and testable from day one. Mirrors the regression
// channel HelloSkillTS provides for the TS path: any future change
// to the Go scaffold body re-runs through the toolchain here, so
// drift between the inline template and what the Go compiler
// accepts is caught at PR time rather than after the operator runs
// `gridctl agent init --lang go`.
//
// Skipped under -short and when the go toolchain is unavailable.
// The test sets up a self-contained module with a replace directive
// pointing back at the gridctl repo so the scaffolded source
// resolves pkg/agent/skill and pkg/agent/llm against the local tree
// rather than fetching from a registry.
func TestScaffoldGoOutputCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go scaffold compile-check in -short mode")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go toolchain not in PATH: %v", err)
	}

	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Fatalf("findRepoRoot: %v", err)
	}

	dir := t.TempDir()
	if _, err := Scaffold(dir, Options{Language: "go", SkillName: "hello-go"}); err != nil {
		t.Fatalf("Scaffold(go): %v", err)
	}

	// Self-contained module that references the local gridctl tree
	// via replace so `go build` resolves pkg/agent/skill without a
	// network fetch. No go.sum needed — local replaces are not
	// checksummed.
	goMod := "module hellogoscaffold\n\n" +
		"go 1.26\n\n" +
		"require github.com/gridctl/gridctl v0.0.0\n\n" +
		"replace github.com/gridctl/gridctl => " + repoRoot + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	// `go mod tidy` resolves the transitive deps of the gridctl
	// module so the build sees a complete graph. Module cache reuse
	// keeps this fast when the test runs alongside the rest of the
	// gridctl test suite.
	tidy := exec.Command(goBin, "mod", "tidy")
	tidy.Dir = dir
	tidy.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy:\n%s\nerr: %v", out, err)
	}

	build := exec.Command(goBin, "build", "./...")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build ./...:\n%s\nerr: %v", out, err)
	}

	test := exec.Command(goBin, "test", "./...")
	test.Dir = dir
	test.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	if out, err := test.CombinedOutput(); err != nil {
		t.Fatalf("go test ./...:\n%s\nerr: %v", out, err)
	}
}

// findRepoRoot walks upward from this test source file until it
// hits the gridctl repo's go.mod. Used to wire a local replace
// directive into the scaffolded skill's go.mod without depending
// on the operator's working directory.
func findRepoRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("scaffold compile-check: runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		mod := filepath.Join(dir, "go.mod")
		if info, err := os.Stat(mod); err == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("scaffold compile-check: no go.mod found walking up")
		}
		dir = parent
	}
}
