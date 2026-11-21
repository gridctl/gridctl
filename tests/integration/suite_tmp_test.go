//go:build integration

package integration

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// suiteTmpPrefix names the per-run root that runIntegrationTests owns.
	// The stale sweep matches only this prefix, never the legacy names an
	// older checkout may still be using concurrently.
	suiteTmpPrefix = "gridctl-integration-"
	// suiteTmpStaleAge is how old a leftover root must be before the sweep
	// removes it. No run lasts this long, so a live root is never touched.
	suiteTmpStaleAge = 24 * time.Hour
)

// suiteTmpRoot is the per-run root directory. runIntegrationTests sets it
// before any test runs and removes it after all tests finish.
var suiteTmpRoot string

// sharedTempDir creates a directory under the per-run root for state that
// must outlive the first calling test, such as a binary built once per
// process. The run removes it after all tests finish.
func sharedTempDir(name string) (string, error) {
	return sharedTempDirIn(suiteTmpRoot, name)
}

func sharedTempDirIn(root, name string) (string, error) {
	if root == "" {
		return "", errors.New("integration suite temp root is not set; shared directories need runIntegrationTests")
	}
	return os.MkdirTemp(root, name+"-*")
}

// sweepStaleSuiteRoots removes leftover per-run roots directly under parent
// whose modification time is before cutoff, such as roots from a killed run.
// It is best effort: errors are logged and never fail the run.
func sweepStaleSuiteRoots(parent string, cutoff time.Time) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		log.Printf("suite temp sweep: reading %s: %v", parent, err)
		return
	}
	for _, e := range entries {
		// DirEntry reports the entry itself, so a symlink is never a directory here.
		if !e.IsDir() || !strings.HasPrefix(e.Name(), suiteTmpPrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			log.Printf("suite temp sweep: stat %s: %v", e.Name(), err)
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(parent, e.Name())
		if err := os.RemoveAll(path); err != nil {
			log.Printf("suite temp sweep: removing %s: %v", path, err)
		}
	}
}

func TestSuiteTmp_SharedTempDirUnderRoot(t *testing.T) {
	dir, err := sharedTempDir("probe")
	if err != nil {
		t.Fatalf("sharedTempDir: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("shared dir %s is not a directory: %v", dir, err)
	}
	if filepath.Dir(dir) != suiteTmpRoot {
		t.Fatalf("shared dir %s is not a child of the suite root %s", dir, suiteTmpRoot)
	}
	if !strings.HasPrefix(filepath.Base(suiteTmpRoot), suiteTmpPrefix) {
		t.Fatalf("suite root %s lacks the %s prefix", suiteTmpRoot, suiteTmpPrefix)
	}
	if !strings.HasPrefix(filepath.Base(dir), "probe-") {
		t.Fatalf("shared dir %s does not carry its name", dir)
	}
}

func TestSuiteTmp_SharedTempDirRefusesEmptyRoot(t *testing.T) {
	dir, err := sharedTempDirIn("", "probe")
	if err == nil {
		t.Fatalf("sharedTempDirIn with an empty root returned %q and no error", dir)
	}
	if dir != "" {
		t.Fatalf("sharedTempDirIn with an empty root returned path %q", dir)
	}

	parent := t.TempDir()
	dir, err = sharedTempDirIn(parent, "probe")
	if err != nil {
		t.Fatalf("sharedTempDirIn: %v", err)
	}
	if filepath.Dir(dir) != parent {
		t.Fatalf("shared dir %s is not a child of %s", dir, parent)
	}
}

func TestSuiteTmp_SweepRemovesOnlyStaleRoots(t *testing.T) {
	parent := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	backdate := func(path string) {
		t.Helper()
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	mkdir := func(name string) string {
		t.Helper()
		path := filepath.Join(parent, name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	stale := mkdir(suiteTmpPrefix + "old")
	if err := os.WriteFile(filepath.Join(stale, "gridctl"), []byte("bin"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(stale)
	mkdir(suiteTmpPrefix + "fresh")
	backdate(mkdir("gridctl-prefeature-1"))
	file := filepath.Join(parent, suiteTmpPrefix+"file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	backdate(file)

	sweepStaleSuiteRoots(parent, time.Now().Add(-suiteTmpStaleAge))

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	want := []string{suiteTmpPrefix + "file", suiteTmpPrefix + "fresh", "gridctl-prefeature-1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after sweep: got %v, want %v", got, want)
	}

	// A missing parent is logged, not fatal.
	sweepStaleSuiteRoots(filepath.Join(parent, "missing"), time.Now())
}

// TestSuiteTmp_NoUnownedTempDirs fails on any temp directory or file created
// in the system temp directory, which nothing would remove. Shared state
// goes through sharedTempDir; per-test state uses t.TempDir.
func TestSuiteTmp_NoUnownedTempDirs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(cwd, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no Go files found in %s", cwd)
	}
	fset := token.NewFileSet()
	var offenders []string
	rootCreations := 0
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			fn, isFunc := decl.(*ast.FuncDecl)
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !emptyParentTempCall(call) {
					return true
				}
				if isFunc && fn.Recv == nil && fn.Name.Name == "runIntegrationTests" {
					rootCreations++
					return true
				}
				pos := fset.Position(call.Pos())
				offenders = append(offenders, filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line))
				return true
			})
		}
	}
	if len(offenders) > 0 {
		t.Errorf("MkdirTemp or CreateTemp with an empty parent directory leaks into the system temp directory; use sharedTempDir or t.TempDir:\n%s", strings.Join(offenders, "\n"))
	}
	if rootCreations != 1 {
		t.Errorf("runIntegrationTests creates the suite root %d times with an empty parent directory, want 1", rootCreations)
	}
}

// emptyParentTempCall reports whether call is os.MkdirTemp or os.CreateTemp
// with an empty string literal as its parent directory.
func emptyParentTempCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "MkdirTemp" && sel.Sel.Name != "CreateTemp") {
		return false
	}
	if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" {
		return false
	}
	if len(call.Args) == 0 {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && s == ""
}
