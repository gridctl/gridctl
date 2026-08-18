package state

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// homeGuardAllowlist names the production files permitted to call
// os.UserHomeDir or read the HOME environment directly. Everything else
// must resolve paths through state.Home() so a GRIDCTL_HOME override is
// honored completely: a partially honored config root silently writes to
// the real home while the user believes their state is sandboxed, which
// is worse than no override at all.
//
//   - pkg/state/home.go: the resolver itself.
//   - pkg/config/loader.go: tilde expansion in user-authored stack.yaml
//     paths, where ~ means the user's actual home.
//   - pkg/runtime/detect.go: ~/.docker context detection; Docker state
//     is machine-level, not gridctl state.
var homeGuardAllowlist = map[string]bool{
	"pkg/state/home.go":     true,
	"pkg/config/loader.go":  true,
	"pkg/runtime/detect.go": true,
}

// TestNoUserHomeDirOutsideResolver walks every production Go file in the
// repository and fails on any os.UserHomeDir call or os.Getenv("HOME") /
// os.Getenv("USERPROFILE") read outside the allowlist. AST-level, not a
// string grep: comments and test files do not trip it.
func TestNoUserHomeDirOutsideResolver(t *testing.T) {
	root := repoRoot(t)
	var violations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// Skip vendored, generated frontend, git internals, and hidden dirs.
			if name == "vendor" || name == "web" || name == "node_modules" || strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if homeGuardAllowlist[rel] {
			return nil
		}
		if v := inspectFile(t, path, rel); len(v) > 0 {
			violations = append(violations, v...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}
	if len(violations) > 0 {
		t.Errorf("home resolution must go through state.Home() (or be allowlisted in homeguard_test.go with a rationale):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

func inspectFile(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", rel, err)
	}
	var violations []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok || pkgIdent.Name != "os" {
			return true
		}
		pos := fset.Position(call.Pos())
		switch sel.Sel.Name {
		case "UserHomeDir":
			violations = append(violations, pos.String()+": os.UserHomeDir()")
		case "Getenv", "LookupEnv":
			if len(call.Args) == 1 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok {
					if lit.Value == `"HOME"` || lit.Value == `"USERPROFILE"` {
						violations = append(violations, pos.String()+": os."+sel.Sel.Name+"("+lit.Value+")")
					}
				}
			}
		}
		return true
	})
	return violations
}

// repoRoot walks up from the package directory to the go.mod root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above package directory")
		}
		dir = parent
	}
}
