package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStore_ReadManifestFile(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveSkill(&AgentSkill{Name: "demo", Description: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteFile("demo", "scripts/run.sh", []byte("safe")); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data, err := s.ReadManifestFile(ctx, "demo", "scripts/run.sh")
	if err != nil || string(data) != "safe" {
		t.Fatalf("read = %q, %v", data, err)
	}
	for _, path := range []string{"../SKILL.md", "/etc/passwd", "scripts", "missing"} {
		if _, err := s.ReadManifestFile(ctx, "demo", path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.ReadManifestFile(canceled, "demo", "scripts/run.sh"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(s.Dir(), "skills", "demo")
	if err := os.Symlink(outside, filepath.Join(base, "linked")); err != nil {
		t.Skip(err)
	}
	if _, err := s.ReadManifestFile(ctx, "demo", "linked/secret"); err == nil {
		t.Fatal("followed directory symlink")
	}
	if err := os.Symlink(filepath.Join(outside, "secret"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadManifestFile(ctx, "demo", "link"); err == nil {
		t.Fatal("followed file symlink")
	}
}

func TestStore_ReadManifestFileConcurrentReplacement(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveSkill(&AgentSkill{Name: "demo", Description: "Demo"}); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(s.Dir(), "skills", "demo")
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "data")
	if err := os.Symlink(secret, file); err != nil {
		t.Skip(err)
	}
	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		defer close(done)
		for range 300 {
			for _, linked := range []bool{false, true} {
				tmp := filepath.Join(base, "swap")
				var err error
				if linked {
					err = os.Symlink(secret, tmp)
				} else {
					err = os.WriteFile(tmp, []byte("safe"), 0600)
				}
				if err != nil {
					errCh <- err
					return
				}
				if err := os.Rename(tmp, file); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()
	for range 500 {
		data, err := s.ReadManifestFile(context.Background(), "demo", "data")
		if err == nil && string(data) != "safe" {
			t.Errorf("escaped root: %q", data)
			break
		}
	}
	<-done
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}
