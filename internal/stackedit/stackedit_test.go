package stackedit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const source = `# heading comment
name: demo
mcp-servers:
  - name: first # inline comment
    image: alpine
`

func TestAppendResources_PreservesCommentsAndAppendsInOrder(t *testing.T) {
	out, err := AppendResources([]byte(source), "mcp-servers",
		[]byte("name: second\nurl: https://a.example.com/mcp\n"),
		[]byte("name: third\ntransport: stdio\ncommand: [echo]\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, want := range []string{"# heading comment", "# inline comment", "name: second", "name: third"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Index(text, "name: second") > strings.Index(text, "name: third") {
		t.Error("snippets appended out of order")
	}
}

func TestAppendResources_CreatesMissingSequence(t *testing.T) {
	out, err := AppendResources([]byte("name: demo\n"), "mcp-servers", []byte("name: only\nimage: alpine\nport: 3000\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "mcp-servers:") || !strings.Contains(string(out), "name: only") {
		t.Errorf("sequence not created:\n%s", out)
	}
}

func TestAppendResources_RejectsBadInput(t *testing.T) {
	if _, err := AppendResources([]byte(source), "mcp-servers"); err == nil {
		t.Error("zero snippets must error")
	}
	if _, err := AppendResources([]byte(source), "mcp-servers", []byte("- a\n- b\n")); err == nil {
		t.Error("non-mapping snippet must error")
	}
	if _, err := AppendResources([]byte("[1,2]"), "mcp-servers", []byte("name: x\n")); err == nil {
		t.Error("non-mapping document must error")
	}
}

func TestRemoveResourceByName_ThenAppendStaysBlockStyle(t *testing.T) {
	removed, err := RemoveResourceByName([]byte(source), "mcp-servers", "first")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(removed), "name: first") {
		t.Errorf("entry not removed:\n%s", removed)
	}

	// Appending into the now-empty (flow-rendered) sequence must come back
	// out in block style, one entry per line.
	out, err := AppendResources(removed, "mcp-servers", []byte("name: fresh\nimage: alpine\nport: 3000\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "mcp-servers: [") {
		t.Errorf("flow style leaked into output:\n%s", out)
	}

	if _, err := RemoveResourceByName([]byte(source), "mcp-servers", "absent"); err == nil {
		t.Error("removing a missing server must error")
	}
}

func TestAtomicWriteAndPathLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte("old"), 0640); err != nil {
		t.Fatal(err)
	}

	mu := PathLock(path)
	mu.Lock()
	err := AtomicWrite(path, []byte("new"))
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Errorf("content = %q err=%v", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0640 {
		t.Errorf("permissions not preserved: %v", info.Mode().Perm())
	}
	if PathLock(path) != mu {
		t.Error("PathLock must return the same mutex per path")
	}
}
