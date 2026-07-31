package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// findCheck returns the check with the given ID, failing if absent.
func findCheck(t *testing.T, checks []doctorCheck, id string) doctorCheck {
	t.Helper()
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no %q check in %+v", id, checks)
	return doctorCheck{}
}

func TestCheckProjectLockfileStates(t *testing.T) {
	writeHomeFile := func(t *testing.T, home, rel, content string) {
		t.Helper()
		path := filepath.Join(home, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("nothing projected", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		var checks []doctorCheck
		checkProjectLockfile(context.Background(), &checks)
		c := findCheck(t, checks, "project.lockfile")
		if c.Status != doctorStatusOK || !strings.Contains(c.Message, "no projection lockfile") {
			t.Errorf("check = %+v", c)
		}
	})

	t.Run("unified in use", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeHomeFile(t, home, filepath.Join(".gridctl", "project.lock.yaml"), "version: 1\nrevision: 1\n")
		var checks []doctorCheck
		checkProjectLockfile(context.Background(), &checks)
		c := findCheck(t, checks, "project.lockfile")
		if c.Status != doctorStatusOK || !strings.Contains(c.Message, "unified project lockfile in use") {
			t.Errorf("check = %+v", c)
		}
	})

	t.Run("legacy pending migration", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeHomeFile(t, home, filepath.Join(".gridctl", "skillsync.lock.yaml"), "version: 1\nprojections: {}\n")
		var checks []doctorCheck
		checkProjectLockfile(context.Background(), &checks)
		c := findCheck(t, checks, "project.lockfile")
		if c.Status != doctorStatusOK || !strings.Contains(c.Message, "legacy projection lockfiles in use") {
			t.Errorf("check = %+v", c)
		}
	})

	t.Run("tombstone without unified file", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		writeHomeFile(t, home, filepath.Join(".gridctl", "context", "context.lock.yaml"), "version: 2\nnote: migrated\n")
		var checks []doctorCheck
		checkProjectLockfile(context.Background(), &checks)
		c := findCheck(t, checks, "project.lockfile")
		if c.Status != doctorStatusFail || !strings.Contains(c.Message, "tombstone") {
			t.Errorf("check = %+v", c)
		}
	})
}
