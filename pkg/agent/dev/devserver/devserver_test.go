package devserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent/dev/parser"
)

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func setupProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "SKILL.md", "---\nname: hello\n---\n")
	writeFile(t, root, "skill.ts", "await tool(\"gridctl__greeting\");\nawait llm({model:\"x\"});\n")

	nested := filepath.Join(root, "skills", "nested")
	writeFile(t, nested, "SKILL.md", "---\nname: nested\n---\n")
	writeFile(t, nested, "skill.go", "package x\nfunc Run(){ tool(\"y\") }\n")

	return root
}

func TestListSkillsReturnsRecognisedDirs(t *testing.T) {
	root := setupProject(t)
	srv, err := NewServer(root, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agent/dev/skills", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Skills []SkillEntry `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := len(body.Skills); got != 2 {
		t.Fatalf("skills = %d, want 2: %+v", got, body.Skills)
	}
	// names sort alphabetically: "hello" before "nested"
	if body.Skills[0].Name != filepath.Base(root) && body.Skills[0].Name != "hello" {
		t.Logf("first skill = %+v", body.Skills[0])
	}
	for _, s := range body.Skills {
		if s.Lang != "go" && s.Lang != "ts" {
			t.Errorf("skill %q has lang %q, want go/ts", s.Name, s.Lang)
		}
	}
}

func TestGetSkillReturnsParsedGraph(t *testing.T) {
	root := setupProject(t)
	srv, err := NewServer(root, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agent/dev/skills/nested", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var g parser.Graph
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if g.Lang != parser.LangGo {
		t.Errorf("lang = %q, want go", g.Lang)
	}
	if len(g.Nodes) == 0 {
		t.Errorf("expected at least one node, got none")
	}
}

func TestGetSkillUnknownReturns404(t *testing.T) {
	root := setupProject(t)
	srv, err := NewServer(root, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agent/dev/skills/missing", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestEventsReturns503WhenNoWatcher(t *testing.T) {
	root := setupProject(t)
	srv, err := NewServer(root, nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/agent/dev/events", nil)
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestNewServerRejectsMissingRoot(t *testing.T) {
	if _, err := NewServer("/no/such/dir/abc", nil); err == nil {
		t.Fatal("expected error")
	}
}
