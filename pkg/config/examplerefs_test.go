package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExampleStacks_PinnedOrExcepted(t *testing.T) {
	root := repoRoot(t)
	exceptions := loadReferenceExceptions(t, filepath.Join(root, "examples", "reference-exceptions.txt"))
	var stacks []string
	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".yaml" {
			return nil
		}
		base := filepath.Base(path)
		switch base {
		case "skills.yaml", "gridctl-pack.yaml", "gateway-remote.yaml":
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if strings.Contains(rel, "/model-policy/") {
			return nil
		}
		stacks = append(stacks, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stacks) == 0 {
		t.Fatal("no example stacks found")
	}
	for path := range exceptions {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Errorf("exception path missing: %s", path)
		}
	}
	for _, rel := range stacks {
		indexed, err := ParseStackIndex(context.Background(), filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("%s: parse: %v", rel, err)
			continue
		}
		for _, issue := range DiagnoseMutableRefs(indexed) {
			if issue.Severity != SeverityWarning {
				continue
			}
			if !strings.HasPrefix(issue.Message, prefixMutableImage) && !strings.HasPrefix(issue.Message, prefixMutablePkg) {
				continue
			}
			if referenceExceptionAllows(exceptions, rel, issue.Field) {
				continue
			}
			t.Errorf("%s: unpinned selector at %s: %s", rel, issue.Field, issue.Message)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func loadReferenceExceptions(t *testing.T, path string) map[string]map[string]bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		file, field, _ := strings.Cut(line, " ")
		file = strings.TrimSpace(file)
		field = strings.TrimSpace(field)
		if out[file] == nil {
			out[file] = map[string]bool{}
		}
		if field == "" {
			out[file]["*"] = true
			continue
		}
		out[file][field] = true
	}
	if len(out) == 0 {
		t.Fatal("no exceptions loaded")
	}
	return out
}

func referenceExceptionAllows(exceptions map[string]map[string]bool, file, field string) bool {
	fields, ok := exceptions[file]
	if !ok {
		return false
	}
	return fields["*"] || fields[field]
}
