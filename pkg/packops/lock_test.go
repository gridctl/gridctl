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

func TestAdd_ExtendsLockRepoMismatch(t *testing.T) {
	mgrs, imp := testEnv(t)
	const (
		baseSHA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		lockRepo = "https://gitlab.example.com/other.git"
		added    = "https://example.com/infra.git"
	)
	seedBase(t, "infra", added, baseSHA)
	manifest := boosterManifest()
	lock := honoredExtendsLock(t, manifest, "infra", lockRepo, baseSHA)
	repo := packFixture(t, manifest, map[string]string{
		pack.LockFileName: lock,
		"servers.yaml":    fragmentYAML(),
	})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(res.Doc.Unresolved, "stack:servers.yaml") {
		t.Fatalf("unresolved = %v", res.Doc.Unresolved)
	}
	want := "base pack 'infra' is added from https://example.com/infra.git, lock expects https://gitlab.example.com/other.git"
	if !containsString(res.Doc.Warnings, want) {
		t.Fatalf("warnings = %v", res.Doc.Warnings)
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	ps, err := findPack(lf, "netops")
	if err != nil {
		t.Fatal(err)
	}
	if ps.Pack.UnresolvedDetails["stack:servers.yaml"] != want {
		t.Fatalf("detail = %q", ps.Pack.UnresolvedDetails["stack:servers.yaml"])
	}
}

func TestAdd_ExtendsLockCommitNote(t *testing.T) {
	mgrs, imp := testEnv(t)
	const (
		baseSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		lockSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		repoURL = "https://example.com/infra.git"
	)
	seedBase(t, "infra", repoURL, baseSHA)
	manifest := boosterManifest()
	lock := honoredExtendsLock(t, manifest, "infra", repoURL, lockSHA)
	repo := packFixture(t, manifest, map[string]string{
		pack.LockFileName: lock,
		"servers.yaml":    fragmentYAML(),
	})
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.Stack == nil || !res.Doc.Stack.Fragment {
		t.Fatalf("stack = %+v", res.Doc.Stack)
	}
	if containsString(res.Doc.Unresolved, "stack:servers.yaml") {
		t.Fatalf("commit mismatch must not unresolved the fragment: %+v", res.Doc)
	}
	want := "base infra at aaaaaaaa, lock expects bbbbbbbb"
	if res.Doc.Lock == nil || !containsString(res.Doc.Lock.Notes, want) {
		t.Fatalf("lock = %+v", res.Doc.Lock)
	}
}

func TestAdd_LockedExtendsCommitRefusesBeforeWrite(t *testing.T) {
	mgrs, imp := testEnv(t)
	const (
		baseSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		lockSHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		repoURL = "https://example.com/infra.git"
	)
	seedBase(t, "infra", repoURL, baseSHA)
	before, err := os.ReadFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	manifest := boosterManifest()
	lock := honoredExtendsLock(t, manifest, "infra", repoURL, lockSHA)
	repo := packFixture(t, manifest, map[string]string{
		pack.LockFileName: lock,
		"servers.yaml":    fragmentYAML(),
	})
	_, err = mgrs.Add(context.Background(), imp, AddOptions{Repo: repo, Locked: true})
	if err == nil || !strings.Contains(err.Error(), "lock is stale (base infra at aaaaaaaa, lock expects bbbbbbbb)") {
		t.Fatalf("err = %v", err)
	}
	after, err := os.ReadFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("skills.lock.yaml changed")
	}
	home, _ := os.UserHomeDir()
	if _, statErr := os.Stat(filepath.Join(home, ".gridctl", "packs", "netops")); !os.IsNotExist(statErr) {
		t.Fatalf("booster checkout created: %v", statErr)
	}
}

func TestLock_CheckDigestAndRemoved(t *testing.T) {
	dir := t.TempDir()
	manifest := []byte("apiVersion: gridctl.dev/v1\nkind: Pack\nname: demo\nsources:\n  alpha:\n    repo: https://example.com/alpha.git\n    ref: main\n")
	if err := os.WriteFile(filepath.Join(dir, pack.ManifestFileName), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	parsed, err := pack.Parse(manifest)
	if err != nil {
		t.Fatal(err)
	}
	sha := "0123456789abcdef0123456789abcdef01234567"
	removed := &pack.LockFile{
		Version: 1, Pack: "demo", ManifestDigest: pack.ManifestDigest(parsed),
		Sources: map[string]pack.LockedSource{
			"alpha": {Repo: "https://example.com/alpha.git", Ref: "main", Commit: sha},
			"beta":  {Repo: "https://example.com/beta.git", Commit: sha},
		},
	}
	if err := pack.WriteLockFile(filepath.Join(dir, pack.LockFileName), removed); err != nil {
		t.Fatal(err)
	}
	doc, err := Lock(context.Background(), LockOptions{Dir: dir, Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != "stale" || doc.Reason != "source beta removed" {
		t.Fatalf("removed = %+v", doc)
	}
	differs := &pack.LockFile{
		Version: 1, Pack: "demo", ManifestDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Sources: map[string]pack.LockedSource{
			"alpha": {Repo: "https://example.com/alpha.git", Ref: "main", Commit: sha},
		},
	}
	if err := pack.WriteLockFile(filepath.Join(dir, pack.LockFileName), differs); err != nil {
		t.Fatal(err)
	}
	doc, err = Lock(context.Background(), LockOptions{Dir: dir, Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Status != "stale" || doc.Reason != "digest differs" {
		t.Fatalf("digest = %+v", doc)
	}
}

func TestAdd_ClearsLockedByWhenNotHonored(t *testing.T) {
	mgrs, imp := testEnv(t)
	url, bare := serveGitFiles(t, map[string]string{
		"skills/from-alpha/SKILL.md": "---\nname: from-alpha\ndescription: Alpha skill\n---\n\nAlpha.\n",
	})
	repoGit, err := git.PlainOpen(bare)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repoGit.Head()
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
	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: packRepo}); err != nil {
		t.Fatal(err)
	}
	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: packRepo, IgnoreLock: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.Lock == nil || res.Doc.Lock.State != "ignored" {
		t.Fatalf("lock = %+v", res.Doc.Lock)
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	member := lf.Sources["team-pack/alpha"]
	if member.LockedBy != "" {
		t.Fatalf("LockedBy = %q, want cleared", member.LockedBy)
	}
}

func boosterManifest() string {
	return `apiVersion: gridctl.dev/v1
kind: Pack
name: netops
stack:
  file: servers.yaml
  extends: infra
`
}

func fragmentYAML() string {
	return "version: \"1\"\nmcp-servers:\n  - name: extra\n    command: [\"echo\", \"extra\"]\n"
}

func honoredExtendsLock(t *testing.T, manifest, name, repo, commit string) string {
	t.Helper()
	return "version: 1\npack: netops\nmanifestDigest: " + digestFor(t, manifest) + "\nextends:\n  " + name + ":\n    repo: " + repo + "\n    commit: " + commit + "\n"
}

func seedBase(t *testing.T, name, repo, sha string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := PackCheckoutDir(home, name, sha)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "version: \"1\"\nname: " + name + "\nmcp-servers:\n  - name: base\n    command: [\"echo\", \"base\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "stack.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		name: {Repo: repo, CommitSHA: sha, Pack: &skills.LockedPack{
			Name:  name,
			Stack: &skills.LockedStack{Path: "stack.yaml", Name: name, CheckoutDir: dir},
		}},
	}}
	if err := skills.WriteLockFile(skills.LockFilePath(), lf); err != nil {
		t.Fatal(err)
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
