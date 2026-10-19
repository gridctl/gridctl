package packops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/gridctl/gridctl/pkg/pack"
	"github.com/gridctl/gridctl/pkg/skills"
)

func TestAdd_HonorsPackLock(t *testing.T) {
	mgrs, imp := testEnv(t)
	url, bare := serveGitFiles(t, map[string]string{
		"skills/from-alpha/SKILL.md": "---\nname: from-alpha\ndescription: Alpha skill\n---\n\nAlpha.\n",
	})
	repo, err := git.PlainOpen(bare)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	sha := head.Hash().String()
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: from-alpha, source: alpha }
sources:
  alpha:
    repo: ` + url + `
    ref: master
`
	lock := "version: 1\npack: team-pack\nmanifestDigest: " + digestFor(t, manifest) + "\nsources:\n  alpha:\n    repo: " + url + "\n    ref: master\n    commit: " + sha + "\n"
	packRepo := packFixture(t, manifest, map[string]string{pack.LockFileName: lock})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: packRepo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.Lock == nil || res.Doc.Lock.State != "honored" {
		t.Fatalf("lock = %+v", res.Doc.Lock)
	}
	if res.Doc.Sources[0].CommitSHA != sha {
		t.Fatalf("commit = %s, want %s", res.Doc.Sources[0].CommitSHA, sha)
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	member := lf.Sources["team-pack/alpha"]
	if member.LockedBy != "team-pack" || member.CommitSHA != sha {
		t.Fatalf("member = %+v", member)
	}
}

func TestAdd_LockedRefusesStaleBeforeWrite(t *testing.T) {
	mgrs, imp := testEnv(t)
	url, _ := serveGitFiles(t, map[string]string{
		"skills/from-alpha/SKILL.md": "---\nname: from-alpha\ndescription: Alpha skill\n---\n\nAlpha.\n",
	})
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: from-alpha, source: alpha }
sources:
  alpha:
    repo: ` + url + `
    ref: master
`
	lock := "version: 1\npack: team-pack\nmanifestDigest: sha256:0000000000000000000000000000000000000000000000000000000000000000\nsources:\n  alpha:\n    repo: " + url + "\n    ref: master\n    commit: 0123456789abcdef0123456789abcdef01234567\n"
	packRepo := packFixture(t, manifest, map[string]string{pack.LockFileName: lock})
	before := skills.LockFilePath()
	_, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: packRepo, Locked: true})
	if err == nil || !strings.Contains(err.Error(), "lock is stale (digest differs)") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(before); !os.IsNotExist(statErr) {
		t.Fatalf("skills.lock.yaml was written: %v", statErr)
	}
	home, _ := os.UserHomeDir()
	if _, statErr := os.Stat(filepath.Join(home, ".gridctl", "packs")); !os.IsNotExist(statErr) {
		t.Fatalf("packs dir created: %v", statErr)
	}
}

func TestLock_CheckSourceAdded(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte("apiVersion: gridctl.dev/v1\nkind: Pack\nname: demo\n# comment\nsources:\n  alpha:\n    repo: https://example.com/alpha.git\n    ref: main\n")
	if err := os.WriteFile(filepath.Join(dir, pack.ManifestFileName), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := Lock(context.Background(), LockOptions{Dir: dir, Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != "stale" || doc.Reason != "lock file is missing" {
		t.Fatalf("check = %+v", doc)
	}
	parsed, err := pack.Parse(manifest)
	if err != nil {
		t.Fatal(err)
	}
	lf := &pack.LockFile{
		Version: 1, Pack: "demo", ManifestDigest: pack.ManifestDigest(parsed),
		Sources: map[string]pack.LockedSource{},
	}
	if err := pack.WriteLockFile(filepath.Join(dir, pack.LockFileName), lf); err != nil {
		t.Fatal(err)
	}
	doc, err = Lock(context.Background(), LockOptions{Dir: dir, Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != "stale" || doc.Reason != "source alpha added" {
		t.Fatalf("check = %+v", doc)
	}
}

func TestLock_RefusesNewerVersionBeforeClone(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte("apiVersion: gridctl.dev/v1\nkind: Pack\nname: demo\nsources:\n  alpha:\n    repo: https://example.com/alpha.git\n    ref: main\n")
	if err := os.WriteFile(filepath.Join(dir, pack.ManifestFileName), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	lock := "version: 2\npack: demo\nmanifestDigest: sha256:ab\nsources:\n  alpha:\n    repo: https://example.com/alpha.git\n    ref: main\n    commit: 0123456789abcdef0123456789abcdef01234567\n"
	if err := os.WriteFile(filepath.Join(dir, pack.LockFileName), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, check := range []bool{false, true} {
		_, err := Lock(context.Background(), LockOptions{Dir: dir, Check: check})
		if err == nil || !strings.Contains(err.Error(), "written by a newer gridctl version") {
			t.Fatalf("check=%v err=%v", check, err)
		}
	}
}

func digestFor(t *testing.T, manifest string) string {
	t.Helper()
	m, err := pack.Parse([]byte(manifest))
	if err != nil {
		t.Fatal(err)
	}
	return pack.ManifestDigest(m)
}
