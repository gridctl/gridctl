package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestWithQuietLoad(t *testing.T) {
	t.Setenv("LIFECYCLE_EMPTY_FIXTURE", "")
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte("name: fixture\ngateway:\n  bind: ${LIFECYCLE_EMPTY_FIXTURE}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if _, err := LoadStack(path, WithQuietLoad()); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatal("quiet candidate loading logged variable hints")
	}
	if _, err := LoadStack(path); err != nil {
		t.Fatal(err)
	}
	if logs.Len() == 0 {
		t.Fatal("normal loading lost its existing hints")
	}
}
