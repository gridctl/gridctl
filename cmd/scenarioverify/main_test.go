package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRun_MissingFlags(t *testing.T) {
	var errBuf bytes.Buffer
	code := run(nil, ioDiscard(), &errBuf)
	if code != 2 {
		t.Fatalf("exit = %d", code)
	}
}

func TestRun_List(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "index.yaml")
	body := []byte(`
version: 1
lanes: [unit]
scenarios:
  - id: sample-case
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: github.com/gridctl/gridctl/pkg/mcp
    test: TestFoo/bar
    lanes: [unit]
    expected_boundary: x
`)
	if err := os.WriteFile(index, body, 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := run([]string{"-list", "-lane", "unit", "-index", index}, &out, ioDiscard())
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "sample-case") || !strings.Contains(out.String(), "^TestFoo$/^bar$") {
		t.Fatalf("list output = %s", out.String())
	}
}

func TestRun_MalformedIndex(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "index.yaml")
	events := filepath.Join(dir, "events.json")
	if err := os.WriteFile(index, []byte("version: 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(events, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errBuf bytes.Buffer
	code := run([]string{"-lane", "unit", "-index", index, "-events", events}, ioDiscard(), &errBuf)
	if code != 2 {
		t.Fatalf("exit = %d stderr=%s", code, errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "MALFORMED_INDEX") {
		t.Fatalf("stderr = %s", errBuf.String())
	}
}

type discard struct{}

func ioDiscard() *discard { return &discard{} }

func (*discard) Write(p []byte) (int, error) { return len(p), nil }
