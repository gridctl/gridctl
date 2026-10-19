package pack

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStackSpec_ScalarRoundTrip(t *testing.T) {
	raw := []byte("apiVersion: gridctl.dev/v1\nkind: Pack\nname: neteng\nstack: stack.yaml\n")
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Stack.File != "stack.yaml" || m.Stack.Extends != "" || m.IsBooster() {
		t.Fatalf("stack = %+v booster=%v", m.Stack, m.IsBooster())
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "stack: stack.yaml\n") {
		t.Fatalf("yaml =\n%s", out)
	}
	if strings.Contains(string(out), "file:") {
		t.Fatalf("scalar stack emitted a mapping:\n%s", out)
	}
}

func TestStackSpec_Mapping(t *testing.T) {
	raw := []byte("apiVersion: gridctl.dev/v1\nkind: Pack\nname: netops\nstack:\n  file: servers.yaml\n  extends: network-eng\n")
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Stack.File != "servers.yaml" || m.Stack.Extends != "network-eng" || !m.IsBooster() {
		t.Fatalf("stack = %+v", m.Stack)
	}
	out, err := yaml.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "extends: network-eng") || !strings.Contains(string(out), "file: servers.yaml") {
		t.Fatalf("yaml =\n%s", out)
	}
}

func TestStackSpec_Rejects(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"missing file", "apiVersion: gridctl.dev/v1\nkind: Pack\nname: netops\nstack:\n  extends: infra\n", "requires file"},
		{"unknown key", "apiVersion: gridctl.dev/v1\nkind: Pack\nname: netops\nstack:\n  file: servers.yaml\n  gateway: true\n", "unknown stack field \"gateway\""},
		{"self extends", "apiVersion: gridctl.dev/v1\nkind: Pack\nname: netops\nstack:\n  file: servers.yaml\n  extends: netops\n", "must not equal the pack name"},
		{"bad extends", "apiVersion: gridctl.dev/v1\nkind: Pack\nname: netops\nstack:\n  file: servers.yaml\n  extends: Infra\n", "lowercase"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseLockFile_NewerVersion(t *testing.T) {
	_, err := ParseLockFile([]byte("version: 2\npack: demo\nmanifestDigest: sha256:ab\n"))
	if err == nil || !errors.Is(err, ErrNewerPackLockVersion) {
		t.Fatal(err)
	}
	want := "pack lock was written by a newer gridctl version (gridctl-pack.lock.yaml is version 2, this gridctl supports 1; upgrade gridctl)"
	if err.Error() != want {
		t.Fatalf("message = %q", err.Error())
	}
}

func TestParseLockFile_Rejects(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"missing version", "pack: demo\n", "positive integer"},
		{"zero version", "version: 0\npack: demo\n", "positive integer"},
		{"userinfo", "version: 1\npack: demo\nmanifestDigest: sha256:ab\nsources:\n  netops:\n    repo: https://token@gitlab.example.com/netops.git\n    commit: 0123456789abcdef0123456789abcdef01234567\n", "userinfo"},
		{"short commit", "version: 1\npack: demo\nmanifestDigest: sha256:ab\nsources:\n  netops:\n    repo: https://gitlab.example.com/netops.git\n    commit: abcdef\n", "40-character"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseLockFile([]byte(tc.yaml))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestWriteLockFile_SortedAndAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LockFileName)
	lf := &LockFile{
		Version:        1,
		Pack:           "demo",
		ManifestDigest: "sha256:abc",
		Sources: map[string]LockedSource{
			"zeta": {Repo: "https://example.com/zeta.git", Commit: "0123456789abcdef0123456789abcdef01234567"},
			"alpha": {Repo: "https://example.com/alpha.git", Ref: "main", Commit: "abcdef0123456789abcdef0123456789abcdef01"},
		},
	}
	if err := WriteLockFile(path, lf); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.HasSuffix(text, "\n") {
		t.Fatal("missing trailing newline")
	}
	if strings.Index(text, "alpha:") > strings.Index(text, "zeta:") {
		t.Fatalf("keys not sorted:\n%s", text)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	got, err := ParseLockFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sources["alpha"].Ref != "main" || got.Pack != "demo" {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestManifestDigest_IgnoresAuthAndComments(t *testing.T) {
	base := `apiVersion: gridctl.dev/v1
kind: Pack
name: demo
# a comment that must not stale the lock
description: changed freely
sources:
  netops:
    repo: https://gitlab.example.com/netops.git
    ref: v2.3.0
    auth:
      method: token
      credential_ref: ${var:GIT_TOKEN}
stack:
  file: servers.yaml
  extends: infra
`
	otherAuth := strings.Replace(base, "GIT_TOKEN", "OTHER", 1)
	noComment := strings.Replace(base, "# a comment that must not stale the lock\n", "", 1)
	a, err := Parse([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Parse([]byte(otherAuth))
	if err != nil {
		t.Fatal(err)
	}
	c, err := Parse([]byte(noComment))
	if err != nil {
		t.Fatal(err)
	}
	if ManifestDigest(a) != ManifestDigest(b) || ManifestDigest(a) != ManifestDigest(c) {
		t.Fatalf("digest changed: %s %s %s", ManifestDigest(a), ManifestDigest(b), ManifestDigest(c))
	}
	if !strings.HasPrefix(ManifestDigest(a), "sha256:") {
		t.Fatalf("digest = %s", ManifestDigest(a))
	}
	noExtends := strings.Replace(base, "  extends: infra\n", "", 1)
	d, err := Parse([]byte(noExtends))
	if err != nil {
		t.Fatal(err)
	}
	if ManifestDigest(d) == ManifestDigest(a) {
		t.Fatal("extends did not change the digest")
	}
}

