package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStore_Generation(t *testing.T) {
	s := NewStore(t.TempDir())
	previous := s.Generation()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		current := s.Generation()
		if current <= previous {
			t.Fatalf("generation did not advance: %d", current)
		}
		previous = current
	}
	check(s.Load())
	check(s.SaveSkill(&AgentSkill{Name: "demo", Description: "Demo", State: StateActive}))
	check(s.WriteFile("demo", "helper.txt", []byte("hello")))
	check(s.RefreshFileCount("demo"))
	check(s.DeleteFile("demo", "helper.txt"))
	check(s.RenameSkill("demo", "renamed"))
	check(s.Load())
	if _, err := s.GetSkill("renamed"); err != nil {
		t.Fatal(err)
	}
	s.ListSkills()
	s.ActiveSkills()
	if s.Generation() != previous {
		t.Fatal("read changed generation")
	}
	check(s.DeleteSkill("renamed"))
}

func TestStore_ListFilesSymlink(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveSkill(&AgentSkill{Name: "demo", Description: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(s.Dir(), "skills", "demo", "link")); err != nil {
		t.Fatal(err)
	}
	files, err := s.ListFiles("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "link" || !files[0].Symlink || files[0].IsDir {
		t.Fatalf("files = %+v", files)
	}
}
