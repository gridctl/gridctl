//go:build unix

package registry

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestStore_ListFilesExcludesSpecialFiles(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveSkill(&AgentSkill{Name: "demo", Description: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(s.Dir(), "skills", "demo", "pipe"), 0600); err != nil {
		t.Fatal(err)
	}
	files, err := s.ListFiles("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("special file listed as document: %+v", files)
	}
}
