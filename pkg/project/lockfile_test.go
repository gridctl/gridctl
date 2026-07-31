package project

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEntry(kind Kind, client, source, path string) *Entry {
	return &Entry{
		Kind:     kind,
		Client:   client,
		Source:   source,
		Path:     path,
		SyncedAt: time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
	}
}

func mustMutate(t *testing.T, s *Store, fn func(l *Lock) error) {
	t.Helper()
	if err := s.Mutate(context.Background(), false, fn); err != nil {
		t.Fatal(err)
	}
}

func TestLockRoundTripAndSorting(t *testing.T) {
	s := NewStore(t.TempDir())
	mustMutate(t, s, func(l *Lock) error {
		for _, e := range []*Entry{
			testEntry(KindSkill, "claude-code", "zeta", "/dest/z"),
			testEntry(KindContext, "gemini", "global", "/dest/g"),
			testEntry(KindSkill, "agents", "alpha", "/dest/a"),
		} {
			if err := l.Set(e); err != nil {
				return err
			}
		}
		return l.Save()
	})

	l, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if e := l.Get(KindSkill, "agents", "alpha"); e == nil || e.Path != "/dest/a" {
		t.Fatalf("Get(alpha) = %+v", e)
	}
	skills := l.Entries(KindSkill)
	if len(skills) != 2 || skills[0].Source != "alpha" || skills[1].Source != "zeta" {
		t.Fatalf("Entries(skill) order = %+v", skills)
	}
	if contexts := l.Entries(KindContext); len(contexts) != 1 || contexts[0].Client != "gemini" {
		t.Fatalf("Entries(context) = %+v", contexts)
	}
}

func TestLockSetReplacesAndRemoveDeletes(t *testing.T) {
	s := NewStore(t.TempDir())
	mustMutate(t, s, func(l *Lock) error {
		if err := l.Set(testEntry(KindSkill, "claude-code", "alpha", "/dest/a")); err != nil {
			return err
		}
		updated := testEntry(KindSkill, "claude-code", "alpha", "/dest/a")
		updated.TreeHash = "sha256:abc"
		if err := l.Set(updated); err != nil {
			return err
		}
		if len(l.file.Projections) != 1 {
			t.Errorf("Set must replace, not append: %d entries", len(l.file.Projections))
		}
		if got := l.Get(KindSkill, "claude-code", "alpha").TreeHash; got != "sha256:abc" {
			t.Errorf("TreeHash = %q", got)
		}
		l.Remove(KindSkill, "claude-code", "alpha")
		if l.Get(KindSkill, "claude-code", "alpha") != nil {
			t.Error("entry must be gone after Remove")
		}
		return l.Save()
	})
}

func TestOneOwnerInvariant(t *testing.T) {
	s := NewStore(t.TempDir())
	mustMutate(t, s, func(l *Lock) error {
		if err := l.Set(testEntry(KindSkill, "claude-code", "alpha", "/dest/shared")); err != nil {
			return err
		}
		err := l.Set(testEntry(KindContext, "claude-code", "global", "/dest/shared"))
		if !errors.Is(err, ErrPathConflict) {
			t.Errorf("expected ErrPathConflict, got %v", err)
		}
		// The same path for a different client is fine.
		if err := l.Set(testEntry(KindContext, "gemini", "global", "/dest/shared")); err != nil {
			t.Errorf("distinct clients may share a path: %v", err)
		}
		// Re-recording the same owner is fine.
		if err := l.Set(testEntry(KindSkill, "claude-code", "alpha", "/dest/shared")); err != nil {
			t.Errorf("re-recording the owner must succeed: %v", err)
		}
		return nil
	})
}

func TestNewerLockVersionRejected(t *testing.T) {
	home := t.TempDir()
	s := NewStore(home)
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(), []byte("version: 99\nrevision: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.Load(context.Background())
	if !errors.Is(err, ErrNewerLockVersion) {
		t.Fatalf("expected ErrNewerLockVersion, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "version 99") || !strings.Contains(err.Error(), "supports 1") {
		t.Errorf("error must name found and supported versions: %v", err)
	}
}

// TestHigherRevisionAndUnknownFieldsSurviveRewrite is the Article XVII
// contract: a file written by a same-version, higher-revision gridctl
// keeps its revision and its unknown fields through a rewrite by this
// binary (the PR #1013 frontmatter lesson applied to the lockfile).
func TestHigherRevisionAndUnknownFieldsSurviveRewrite(t *testing.T) {
	home := t.TempDir()
	s := NewStore(home)
	content := `version: 1
revision: 7
future_top_level: keep-me
projections:
    - kind: skill
      client: claude-code
      source: alpha
      path: /dest/a
      channel: symlink
      created_by_gridctl: true
      synced_at: 2026-07-30T12:00:00Z
      future_entry_field: also-keep-me
`
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path(), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	mustMutate(t, s, func(l *Lock) error {
		if err := l.Set(testEntry(KindSkill, "claude-code", "beta", "/dest/b")); err != nil {
			return err
		}
		return l.Save()
	})

	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{"revision: 7", "future_top_level: keep-me", "future_entry_field: also-keep-me", "source: beta"} {
		if !strings.Contains(out, want) {
			t.Errorf("rewritten lockfile lost %q:\n%s", want, out)
		}
	}
}

// TestSetCarriesUnknownFieldsThroughReRecord pins the Article XVII
// property finding its way through the common path: an entry re-recorded
// by this binary (which builds fresh entries with nil Extra) keeps the
// unknown fields a newer revision wrote, and an explicit empty map
// clears them.
func TestSetCarriesUnknownFieldsThroughReRecord(t *testing.T) {
	s := NewStore(t.TempDir())
	mustMutate(t, s, func(l *Lock) error {
		withExtra := testEntry(KindSkill, "claude-code", "alpha", "/dest/a")
		withExtra.Extra = map[string]any{"future_entry_field": "keep-me"}
		if err := l.Set(withExtra); err != nil {
			return err
		}
		return l.Save()
	})

	mustMutate(t, s, func(l *Lock) error {
		// A fresh record, as recordSync/record build on every re-sync.
		if err := l.Set(testEntry(KindSkill, "claude-code", "alpha", "/dest/a")); err != nil {
			return err
		}
		return l.Save()
	})
	l, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Get(KindSkill, "claude-code", "alpha").Extra["future_entry_field"]; got != "keep-me" {
		t.Fatalf("re-record dropped the unknown field: %v", got)
	}

	mustMutate(t, s, func(l *Lock) error {
		cleared := testEntry(KindSkill, "claude-code", "alpha", "/dest/a")
		cleared.Extra = map[string]any{}
		if err := l.Set(cleared); err != nil {
			return err
		}
		return l.Save()
	})
	l, err = s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if extra := l.Get(KindSkill, "claude-code", "alpha").Extra; len(extra) != 0 {
		t.Errorf("an explicit empty map must clear unknown fields, got %v", extra)
	}
}

func TestStaleBackups(t *testing.T) {
	if got := StaleBackups([]string{"b", "a"}, 3); got != nil {
		t.Errorf("within keep limit, nothing is stale: %v", got)
	}
	got := StaleBackups([]string{"20260703-000000", "20260701-000000", "20260702-000000"}, 1)
	if len(got) != 2 || got[0] != "20260701-000000" || got[1] != "20260702-000000" {
		t.Errorf("StaleBackups = %v, want the two oldest in order", got)
	}
}

func TestConcurrentMutateAcrossStores(t *testing.T) {
	home := t.TempDir()
	// Two stores over one home model the CLI and the daemon; the flock
	// alone serializes them.
	a, b := NewStore(home), NewStore(home)
	done := make(chan error, 20)
	for i := 0; i < 10; i++ {
		go func(n int) {
			done <- a.Mutate(context.Background(), false, func(l *Lock) error {
				if err := l.Set(testEntry(KindSkill, "claude-code", "alpha", "/dest/a")); err != nil {
					return err
				}
				return l.Save()
			})
		}(i)
		go func(n int) {
			done <- b.Mutate(context.Background(), false, func(l *Lock) error {
				if err := l.Set(testEntry(KindContext, "gemini", "global", "/dest/g")); err != nil {
					return err
				}
				return l.Save()
			})
		}(i)
	}
	for i := 0; i < 20; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	l, err := a.Load(context.Background())
	if err != nil {
		t.Fatalf("lockfile corrupt after concurrent access: %v", err)
	}
	if l.Get(KindSkill, "claude-code", "alpha") == nil || l.Get(KindContext, "gemini", "global") == nil {
		t.Error("lockfile lost entries under concurrency")
	}
}

func TestSaveOutsideMutateIsRefused(t *testing.T) {
	s := NewStore(t.TempDir())
	l, err := s.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Save(); err == nil {
		t.Error("Save on a read-only lock must be refused")
	}
}
