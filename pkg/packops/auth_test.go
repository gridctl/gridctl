package packops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/skills"
)

// wiringOnlyManifest selects no skills and no agents, so Add's import branch
// never runs. That is the case recordLockedPack has to create the lockfile
// source itself, which is where the credential reference used to be dropped.
const wiringOnlyManifest = `apiVersion: gridctl.dev/v1
kind: Pack
name: wiring-pack
version: 1.0.0
description: Gateway wiring only
author:
  name: Acme Platform
skills: []
agents: []
wiring: true
`

const credentialRef = "${var:GIT_TOKEN}"

// packAuth is the AuthConfig these tests import with. Method is deliberately
// empty: the fixtures are local paths, and an explicit "token" method would be
// refused with ErrProtocolMismatch before the clone (HTTPSTokenAuth requires an
// https:// URL, correctly). Persistence reads only CredentialRef, so leaving
// the method ambient exercises the recording path without needing a network
// remote. Token is carried to prove it is not what gets written.
func packAuth(token string) skills.AuthConfig {
	return skills.AuthConfig{Token: token, CredentialRef: credentialRef}
}

// lockedSourceFor reads back the lockfile record Add wrote for a repo.
func lockedSourceFor(t *testing.T, repo string) skills.LockedSource {
	t.Helper()
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatalf("read lockfile: %v", err)
	}
	src, ok := lf.Sources[skills.RepoToName(repo)]
	if !ok {
		t.Fatalf("no lockfile source recorded for %q (have %v)", repo, lf.Sources)
	}
	return src
}

func TestAdd_PersistsCredentialRefForImportedPack(t *testing.T) {
	mgrs, imp := testEnv(t)
	repo := packFixture(t, testManifest, nil)

	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo, Auth: packAuth("resolved-secret")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	src := lockedSourceFor(t, repo)
	if src.CredentialRef != credentialRef {
		t.Errorf("lockfile CredentialRef = %q, want %q", src.CredentialRef, credentialRef)
	}

	// The origin sidecars are the other half: skill update re-resolves from
	// them, not from the lockfile. Both kinds get one, and the agent write is
	// a separate call site from the skill write.
	registry := filepath.Join(mgrs.Home, ".gridctl", "skills")
	for _, dir := range []string{
		filepath.Join(registry, "skills", "alpha"),
		filepath.Join(registry, "agents", "reviewer"),
	} {
		origin, err := skills.ReadOrigin(dir)
		if err != nil {
			t.Fatalf("read origin %s: %v", dir, err)
		}
		if origin.CredentialRef != credentialRef {
			t.Errorf("%s origin CredentialRef = %q, want %q", dir, origin.CredentialRef, credentialRef)
		}
	}
}

func TestAdd_PersistsCredentialRefForWiringOnlyPack(t *testing.T) {
	mgrs, imp := testEnv(t)
	repo := packFixture(t, wiringOnlyManifest, nil)

	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo, Auth: packAuth("resolved-secret")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Nothing imported, so recordLockedPack built this source from scratch.
	src := lockedSourceFor(t, repo)
	if src.CredentialRef != credentialRef {
		t.Errorf("wiring-only pack lost its CredentialRef: got %q, want %q", src.CredentialRef, credentialRef)
	}
}

func TestAdd_NeverPersistsTheTokenItself(t *testing.T) {
	mgrs, imp := testEnv(t)
	repo := packFixture(t, testManifest, nil)

	const secret = "ghp_thisMustNotBeWrittenAnywhere"
	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo, Auth: packAuth(secret)}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Article VII / XVI: only the reference may land on disk. Walk everything
	// the import wrote rather than checking the two files we happen to know
	// about, so a new persistence site cannot quietly start leaking.
	root := filepath.Join(mgrs.Home, ".gridctl")
	var found []string
	walkFiles(t, root, func(path string, content []byte) {
		if strings.Contains(string(content), secret) {
			found = append(found, path)
		}
	})
	if len(found) > 0 {
		t.Errorf("token value written to disk in: %v", found)
	}
}

func TestAdd_ZeroAuthRecordsNoCredentialRef(t *testing.T) {
	mgrs, imp := testEnv(t)
	repo := packFixture(t, testManifest, nil)

	// A public pack must be byte-for-byte unchanged in what it records.
	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if src := lockedSourceFor(t, repo); src.CredentialRef != "" {
		t.Errorf("CredentialRef = %q, want empty for an unauthenticated import", src.CredentialRef)
	}
}

func TestPreview_AcceptsAuthWithoutPersisting(t *testing.T) {
	testEnv(t)
	repo := packFixture(t, testManifest, nil)

	res, err := Preview(context.Background(), PreviewOptions{Repo: repo, Auth: packAuth("resolved-secret")})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if res.Pack != "team-pack" {
		t.Errorf("Pack = %q, want team-pack", res.Pack)
	}
	// Preview writes nothing, so there must be no lockfile source at all.
	if lf, err := skills.ReadLockFile(skills.LockFilePath()); err == nil {
		if _, ok := lf.Sources[skills.RepoToName(repo)]; ok {
			t.Error("Preview recorded a lockfile source; it must write nothing")
		}
	}
}

// walkFiles visits every regular file under root, skipping .git internals.
func walkFiles(t *testing.T, root string, visit func(path string, content []byte)) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // a missing tree just means nothing was written there
		}
		if info.IsDir() {
			if info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		visit(path, content)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// The wire shape matters, not the Go value: encoding/json turns a nil slice
// into null, and every client is typed for an array. A pack whose selection
// resolves to nothing (or whose import produces no progress notes) used to
// send null and crash the import wizard's success step.
func TestAdd_ListFieldsMarshalAsEmptyArraysNotNull(t *testing.T) {
	mgrs, imp := testEnv(t)
	// A manifest naming resources the repo does not ship resolves to nothing,
	// leaving the selection slices nil. An empty manifest list would not do:
	// it means "everything discovered" and resolves to a populated set.
	unresolvableManifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: ghost-pack
version: 1.0.0
description: Selects nothing that exists
author:
  name: Test
skills: [ghost]
agents: [phantom]
wiring: true
`
	repo := packFixture(t, unresolvableManifest, nil)

	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	encoded, err := json.Marshal(map[string]any{"doc": res.Doc, "notes": res.Notes})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(encoded)

	for _, nulled := range []string{`"skills":null`, `"agents":null`, `"notes":null`} {
		if strings.Contains(body, nulled) {
			t.Errorf("response carries %s; a nil slice reached the wire: %s", nulled, body)
		}
	}
	for _, empty := range []string{`"skills":[]`, `"agents":[]`, `"notes":[]`} {
		if !strings.Contains(body, empty) {
			t.Errorf("expected %s in the response, got: %s", empty, body)
		}
	}
}

// A populated response must serialize exactly as before.
func TestAdd_PopulatedListsAreUnchanged(t *testing.T) {
	mgrs, imp := testEnv(t)
	repo := packFixture(t, testManifest, nil)

	res, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	encoded, _ := json.Marshal(res.Doc)
	body := string(encoded)
	if !strings.Contains(body, `"skills":["alpha"]`) {
		t.Errorf("populated skills changed shape: %s", body)
	}
}
