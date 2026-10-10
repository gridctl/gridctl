package packops

import (
	"context"
	"log/slog"
	"net/http/cgi" //nolint:gosec // G504: Httpoxy fixed in Go 1.6.3; test-only git http-backend
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/project"
	"github.com/gridctl/gridctl/pkg/skills"
)

func serveGitFiles(t *testing.T, files map[string]string) (url, bare string) {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("git binary required")
	}
	work := t.TempDir()
	repo, err := git.PlainInit(work, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wt.Commit("initial", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
	bareParent := t.TempDir()
	bare = filepath.Join(bareParent, "repo.git")
	if _, err := git.PlainClone(bare, true, &git.CloneOptions{URL: work}); err != nil {
		t.Fatal(err)
	}
	handler := &cgi.Handler{
		Path: gitBin,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + bareParent, "GIT_HTTP_EXPORT_ALL=1"},
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL + "/repo.git", bare
}

func pushFiles(t *testing.T, bare string, files map[string]string) {
	t.Helper()
	work := t.TempDir()
	repo, err := git.PlainClone(work, false, &git.CloneOptions{URL: bare})
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for path, content := range files {
		full := filepath.Join(work, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := wt.Commit("more", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(&git.PushOptions{}); err != nil {
		t.Fatal(err)
	}
}

func TestAdd_ExternalSourcesPinsAndImports(t *testing.T) {
	mgrs, imp := testEnv(t)
	ctx := context.Background()
	alphaURL, _ := serveGitFiles(t, map[string]string{
		"skills/from-alpha/SKILL.md":  "---\nname: from-alpha\ndescription: Alpha skill\n---\n\nAlpha body.\n",
		"skills/extra-alpha/SKILL.md": "---\nname: extra-alpha\ndescription: Not selected\n---\n\nExtra.\n",
		"agents/alpha-agent.md":       "---\nname: alpha-agent\ndescription: Alpha agent\n---\n\nAgent.\n",
	})
	betaURL, betaBare := serveGitFiles(t, map[string]string{
		"skills/from-beta/SKILL.md": "---\nname: from-beta\ndescription: Beta skill\n---\n\nBeta body.\n",
		"agents/beta-agent.md":      "---\nname: beta-agent\ndescription: Beta agent\n---\n\nAgent.\n",
	})
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - alpha
  - { name: from-alpha, source: zeta }
  - { name: from-beta, source: alpha }
agents:
  - { name: beta-agent, source: alpha }
sources:
  zeta:
    repo: ` + alphaURL + `
    ref: master
  alpha:
    repo: ` + betaURL + `
    ref: master
`
	repo := packFixture(t, manifest, nil)
	res, err := mgrs.Add(ctx, imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Doc.Sources) != 2 || res.Doc.Sources[0].Name != "alpha" || res.Doc.Sources[1].Name != "zeta" {
		t.Fatalf("sources = %+v, want name order alpha then zeta", res.Doc.Sources)
	}
	if res.Doc.Sources[0].Error != "" || res.Doc.Sources[0].CommitSHA == "" {
		t.Fatalf("alpha summary = %+v", res.Doc.Sources[0])
	}

	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "version: 7") {
		t.Fatalf("lockfile stamp = %q, want version 7", firstLine(string(raw)))
	}
	_, packSrc, ok := lf.FindPackSource("team-pack")
	if !ok || packSrc.Pack == nil || len(packSrc.Pack.Sources) != 2 {
		t.Fatalf("pack sources = %+v", packSrc)
	}
	members := lf.MemberSources("team-pack")
	if len(members) != 2 || members[0] != "team-pack/alpha" || members[1] != "team-pack/zeta" {
		t.Fatalf("members = %v", members)
	}
	for _, key := range members {
		if lf.Sources[key].Pack != nil || lf.Sources[key].PackMember != "team-pack" {
			t.Fatalf("member %s = %+v", key, lf.Sources[key])
		}
	}
	home, _ := os.UserHomeDir()
	origin, err := skills.ReadOrigin(filepath.Join(home, ".gridctl", "skills", "skills", "from-beta"))
	if err != nil {
		t.Fatal(err)
	}
	if origin.Repo != betaURL || origin.CommitSHA == "" {
		t.Fatalf("origin = %+v, want %s", origin, betaURL)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "skills", "skills", "extra-alpha", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("unselected skill was imported: %v", err)
	}

	// skill update refreshes the recorded set only.
	pushFiles(t, betaBare, map[string]string{
		"skills/from-beta/SKILL.md": "---\nname: from-beta\ndescription: Beta skill\n---\n\nBeta updated.\n",
		"skills/new-beta/SKILL.md":  "---\nname: new-beta\ndescription: New\n---\n\nNew.\n",
	})
	if _, err := imp.Update(ctx, "from-beta", false, true, true); err != nil {
		t.Fatal(err)
	}
	lf, err = skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	member := lf.Sources["team-pack/alpha"]
	if member.PackMember != "team-pack" || member.Pack != nil {
		t.Fatalf("member after update = %+v", member)
	}
	if _, ok := member.Skills["from-beta"]; !ok || len(member.Skills) != 1 {
		t.Fatalf("member skills = %+v", member.Skills)
	}
	if _, ok := member.Agents["beta-agent"]; !ok {
		t.Fatalf("member agents = %+v", member.Agents)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "skills", "skills", "new-beta")); !os.IsNotExist(err) {
		t.Fatalf("update imported an unrecorded skill: %v", err)
	}

	mgrs, _ = freshEnv(t, home)
	applyDoc, err := mgrs.Apply(ctx, "team-pack", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if applyDoc.Applied == 0 {
		t.Fatalf("apply = %+v", applyDoc.Rows)
	}
	store, err := project.NewStore(home).Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tagged := false
	for _, e := range store.Entries(project.KindSkill) {
		if e.Source == "from-beta" && e.Pack == "team-pack" {
			tagged = true
		}
	}
	if !tagged {
		t.Fatal("member skill was not projected with the pack tag")
	}

	mgrs, imp = freshEnv(t, home)
	if _, err := mgrs.Remove(ctx, imp, "team-pack", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	lf, err = skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if keys := lf.MemberSources("team-pack"); len(keys) != 0 {
		t.Fatalf("members after remove = %v", keys)
	}
}

func TestAdd_FailedSourceDoesNotAbortEarlierSource(t *testing.T) {
	mgrs, imp := testEnv(t)
	ctx := context.Background()
	goodURL, _ := serveGitFiles(t, map[string]string{
		"skills/good-skill/SKILL.md": "---\nname: good-skill\ndescription: Good\n---\n\nGood.\n",
	})
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: good-skill, source: good }
  - { name: missing-skill, source: bad }
sources:
  bad:
    repo: http://127.0.0.1:1/no-such.git
  good:
    repo: ` + goodURL + `
`
	repo := packFixture(t, manifest, nil)
	res, err := mgrs.Add(ctx, imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Doc.Unresolved) == 0 || res.Doc.Sources[0].Name != "bad" || res.Doc.Sources[0].Error == "" {
		t.Fatalf("doc = %+v", res.Doc)
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	member := lf.Sources["team-pack/good"]
	if member.PackMember != "team-pack" || len(member.Skills) != 1 {
		t.Fatalf("earlier source = %+v", member)
	}
	bad := lf.Sources["team-pack/bad"]
	if bad.PackMember != "team-pack" || len(bad.Skills) != 0 {
		t.Fatalf("failed source = %+v", bad)
	}
	_, src, ok := lf.FindPackSource("team-pack")
	if !ok || src.Pack.Sources["bad"].Repo == "" || len(src.Pack.Sources["bad"].Skills) != 0 {
		t.Fatalf("pack record = %+v", src.Pack)
	}
}

func TestAdd_NestedPackAndImportError(t *testing.T) {
	mgrs, imp := testEnv(t)
	ctx := context.Background()
	nestedURL, _ := serveGitFiles(t, map[string]string{
		"gridctl-pack.yaml":           "apiVersion: gridctl.dev/v1\nkind: Pack\nname: inner\n",
		"skills/inner-skill/SKILL.md": "---\nname: inner-skill\ndescription: Inner\n---\n\nInner.\n",
	})
	goodURL, _ := serveGitFiles(t, map[string]string{
		"skills/kept-skill/SKILL.md": "---\nname: kept-skill\ndescription: Kept\n---\n\nKept.\n",
	})
	clashURL, _ := serveGitFiles(t, map[string]string{
		"skills/taken/SKILL.md": "---\nname: taken\ndescription: Taken\n---\n\nTaken.\n",
	})
	local := filepath.Join(t.TempDir(), "taken")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(local, "SKILL.md"), []byte("---\nname: taken\ndescription: Local\n---\n\nLocal.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := imp.Import(ctx, skills.ImportOptions{Repo: local, Kind: skills.SourceKindLocal, Trust: true}); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: kept-skill, source: good }
  - { name: inner-skill, source: nested }
  - { name: taken, source: clash }
sources:
  clash:
    repo: ` + clashURL + `
  good:
    repo: ` + goodURL + `
  nested:
    repo: ` + nestedURL + `
`
	repo := packFixture(t, manifest, nil)
	res, err := mgrs.Add(ctx, imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	var nestedErr, clashErr string
	for _, src := range res.Doc.Sources {
		if src.Name == "nested" {
			nestedErr = src.Error
		}
		if src.Name == "clash" {
			clashErr = src.Error
		}
	}
	if !strings.Contains(nestedErr, "itself a pack") {
		t.Fatalf("nested error = %q", nestedErr)
	}
	if clashErr == "" {
		t.Fatal("expected the clashing source import to fail")
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if len(lf.Sources["team-pack/good"].Skills) != 1 {
		t.Fatalf("good source = %+v", lf.Sources["team-pack/good"])
	}
	if _, src, ok := lf.FindPackSource("team-pack"); !ok || len(src.Pack.Sources["clash"].Skills) != 0 {
		t.Fatalf("clash record = %+v", src)
	}
}

func TestAdd_AgentsOnlyAndSkillsOnly(t *testing.T) {
	mgrs, imp := testEnv(t)
	ctx := context.Background()
	url, _ := serveGitFiles(t, map[string]string{
		"skills/only-skill/SKILL.md": "---\nname: only-skill\ndescription: Skill\n---\n\nSkill.\n",
		"agents/only-agent.md":       "---\nname: only-agent\ndescription: Agent\n---\n\nAgent.\n",
	})
	agentURL, _ := serveGitFiles(t, map[string]string{
		"skills/other-skill/SKILL.md": "---\nname: other-skill\ndescription: Other\n---\n\nOther.\n",
		"agents/only-agent.md":        "---\nname: only-agent\ndescription: Agent\n---\n\nAgent.\n",
	})
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: only-skill, source: skills-only }
agents:
  - { name: only-agent, source: agents-only }
sources:
  agents-only:
    repo: ` + agentURL + `
  skills-only:
    repo: ` + url + `
`
	repo := packFixture(t, manifest, nil)
	if _, err := mgrs.Add(ctx, imp, AddOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	skillsSrc := lf.Sources["team-pack/skills-only"]
	if len(skillsSrc.Skills) != 1 || len(skillsSrc.Agents) != 0 {
		t.Fatalf("skills-only = %+v", skillsSrc)
	}
	agentsSrc := lf.Sources["team-pack/agents-only"]
	if len(agentsSrc.Agents) != 1 || len(agentsSrc.Skills) != 0 {
		t.Fatalf("agents-only = %+v", agentsSrc)
	}
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "skills", "skills", "other-skill")); !os.IsNotExist(err) {
		t.Fatalf("agents-only source imported a skill: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "registry", "agents", "only-agent", "AGENT.md")); err != nil {
		// agents live under the registry dir the importer was given
		if _, err2 := os.Stat(filepath.Join(home, ".gridctl", "skills", "agents", "only-agent", "AGENT.md")); err2 != nil {
			t.Fatalf("agent missing: %v / %v", err, err2)
		}
	}
}

func TestAdd_CommitRecheckRestoresAndRejectsWrongHead(t *testing.T) {
	mgrs, imp := testEnv(t)
	ctx := context.Background()
	url, _ := serveGitFiles(t, map[string]string{
		"skills/pinned/SKILL.md": "---\nname: pinned\ndescription: Pinned\n---\n\nOriginal.\n",
	})
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: pinned, source: netops }
sources:
  netops:
    repo: ` + url + `
`
	repo := packFixture(t, manifest, nil)
	cache, err := builder.URLToPath(url)
	if err != nil {
		t.Fatal(err)
	}
	beforeExternalImport = func(name string, clone *skills.CloneResult) {
		r, err := git.PlainOpen(cache)
		if err != nil {
			t.Fatal(err)
		}
		wt, err := r.Worktree()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(cache, "skills", "pinned", "SKILL.md")
		if err := os.WriteFile(path, []byte("---\nname: pinned\ndescription: Pinned\n---\n\nMutated.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add("skills/pinned/SKILL.md"); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Commit("mutate", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeExternalImport = nil })
	res, err := mgrs.Add(ctx, imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.Sources[0].Error != "" {
		t.Fatalf("re-check should restore the pin, got %s", res.Doc.Sources[0].Error)
	}
	home, _ := os.UserHomeDir()
	body, err := os.ReadFile(filepath.Join(home, ".gridctl", "skills", "skills", "pinned", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Mutated") || !strings.Contains(string(body), "Original") {
		t.Fatalf("imported the mutated tree: %s", body)
	}

	mgrs, imp = testEnv(t)
	url, _ = serveGitFiles(t, map[string]string{
		"skills/pinned/SKILL.md": "---\nname: pinned\ndescription: Pinned\n---\n\nOriginal.\n",
	})
	manifest = `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: pinned, source: netops }
sources:
  netops:
    repo: ` + url + `
`
	repo = packFixture(t, manifest, nil)
	cache, err = builder.URLToPath(url)
	if err != nil {
		t.Fatal(err)
	}
	beforeExternalImport = func(name string, clone *skills.CloneResult) {
		r, err := git.PlainOpen(cache)
		if err != nil {
			t.Fatal(err)
		}
		wt, err := r.Worktree()
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(cache, "skills", "pinned", "SKILL.md")
		if err := os.WriteFile(path, []byte("---\nname: pinned\ndescription: Pinned\n---\n\nWrong.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add("skills/pinned/SKILL.md"); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Commit("wrong", &git.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@test.com"}}); err != nil {
			t.Fatal(err)
		}
		_ = r.DeleteRemote("origin")
		_, _ = r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{"http://127.0.0.1:1/missing.git"}})
	}
	res, err = mgrs.Add(ctx, imp, AddOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Doc.Sources[0].Error == "" || len(res.Doc.Unresolved) == 0 {
		t.Fatalf("wrong head should be unresolved, doc = %+v", res.Doc)
	}
	home, _ = os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "skills", "skills", "pinned")); !os.IsNotExist(err) {
		t.Fatalf("wrong tree was imported: %v", err)
	}
}

func TestAdd_SourceAuthPersistsReferenceNotToken(t *testing.T) {
	mgrs, imp := testEnv(t)
	url, _ := serveGitFiles(t, map[string]string{
		"skills/tok-skill/SKILL.md": "---\nname: tok-skill\ndescription: Tok\n---\n\nTok.\n",
	})
	var seen skills.AuthConfig
	orig := cloneAndDiscover
	cloneAndDiscover = func(repo, ref, path string, auth skills.AuthConfig, logger *slog.Logger) (*skills.CloneResult, error) {
		if repo == url {
			seen = auth
			auth = skills.AuthConfig{}
		}
		return orig(repo, ref, path, auth, logger)
	}
	t.Cleanup(func() { cloneAndDiscover = orig })
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: tok-skill, source: netops }
sources:
  netops:
    repo: ` + url + `
`
	repo := packFixture(t, manifest, nil)
	// The pack repository is a local path, so a token on the primary auth
	// cannot clone it. The fan-out rule itself is unit-tested. This test
	// checks that a per-source vault reference is used for the clone and
	// persisted as a reference, never as the resolved token.
	if _, err := mgrs.Add(context.Background(), imp, AddOptions{
		Repo: repo,
		SourceAuth: map[string]skills.AuthConfig{
			"netops": {Method: "token", Token: "resolved-source-token", CredentialRef: "${var:NETOPS}"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if seen.CredentialRef != "${var:NETOPS}" || seen.Token != "resolved-source-token" {
		t.Fatalf("source clone auth = %+v", seen)
	}
	raw, err := os.ReadFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "resolved-source-token") {
		t.Fatal("token landed in the lockfile")
	}
	if !strings.Contains(string(raw), "${var:NETOPS}") {
		t.Fatal("source credential reference was not persisted")
	}
}

func TestPreview_SourceFailureIsUnresolved(t *testing.T) {
	url, _ := serveGitFiles(t, map[string]string{
		"skills/preview-skill/SKILL.md": "---\nname: preview-skill\ndescription: Preview\n---\n\nPreview.\n",
	})
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: preview-skill, source: good }
  - { name: gone, source: bad }
sources:
  bad:
    repo: http://127.0.0.1:1/missing.git
  good:
    repo: ` + url + `
`
	repo := packFixture(t, manifest, nil)
	res, err := Preview(context.Background(), PreviewOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 || res.Sources[0].Error == "" {
		t.Fatalf("preview sources = %+v", res.Sources)
	}
	found := false
	for _, sk := range res.Skills {
		if sk.Name == "preview-skill" && sk.Source == "good" {
			found = true
		}
	}
	if !found {
		t.Fatalf("preview skills = %+v", res.Skills)
	}
}

func TestStatuses_SourceRows(t *testing.T) {
	mgrs, imp := testEnv(t)
	ctx := context.Background()
	url, _ := serveGitFiles(t, map[string]string{
		"skills/status-skill/SKILL.md": "---\nname: status-skill\ndescription: Status\n---\n\nStatus.\n",
	})
	agentURL, _ := serveGitFiles(t, map[string]string{
		"agents/status-agent.md": "---\nname: status-agent\ndescription: Agent\n---\n\nAgent.\n",
	})
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills:
  - { name: status-skill, source: zeta }
agents:
  - { name: status-agent, source: alpha }
sources:
  alpha:
    repo: ` + agentURL + `
  zeta:
    repo: ` + url + `
    auth:
      method: token
      credential_ref: ${var:GIT_TOKEN}
`
	repo := packFixture(t, manifest, nil)
	if _, err := mgrs.Add(ctx, imp, AddOptions{
		Repo:     repo,
		Resolver: func(ref string) (string, error) { return "tok", nil },
	}); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	cachePath := filepath.Join(home, ".gridctl", "cache", "skill-updates.yaml")
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	sha := lf.Sources["team-pack/zeta"].CommitSHA
	status := &skills.UpdateStatus{Updates: map[string]skills.SkillUpdate{
		"status-skill": {CurrentSHA: sha, LatestSHA: sha + "ff", Repo: url, Ref: "master"},
	}}
	if err := skills.WriteUpdateCacheAt(cachePath, status); err != nil {
		t.Fatal(err)
	}
	mgrs, _ = freshEnv(t, home)
	rows, err := mgrs.Statuses(ctx, StatusOptions{Pack: "team-pack"})
	if err != nil {
		t.Fatal(err)
	}
	sourceRows := rowsOfKind(rows[0].Rows, "source")
	if len(sourceRows) != 2 || sourceRows[0].Name != "alpha" || sourceRows[1].Name != "zeta" {
		t.Fatalf("source rows = %+v", sourceRows)
	}
	if sourceRows[0].State != "in-sync" || !strings.Contains(sourceRows[0].Detail, "freshness not checked") {
		t.Fatalf("agents-only row = %+v", sourceRows[0])
	}
	if sourceRows[1].State != "in-sync" || !strings.Contains(sourceRows[1].Detail, "freshness not checked") {
		t.Fatalf("credential source should not be stale, row = %+v", sourceRows[1])
	}
	if rows[0].Info.Counts.Sources != 2 || len(rows[0].Info.Sources) != 2 {
		t.Fatalf("info sources = %+v counts=%+v", rows[0].Info.Sources, rows[0].Info.Counts)
	}

	// A source without a credential ref reports stale from the cache.
	manifest = `apiVersion: gridctl.dev/v1
kind: Pack
name: other-pack
skills:
  - { name: status-skill, source: zeta }
sources:
  zeta:
    repo: ` + url + `
`
	repo = packFixture(t, manifest, nil)
	mgrs, imp = freshEnv(t, home)
	if _, err := mgrs.Add(ctx, imp, AddOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	mgrs, _ = freshEnv(t, home)
	rows, err = mgrs.Statuses(ctx, StatusOptions{Pack: "other-pack"})
	if err != nil {
		t.Fatal(err)
	}
	sourceRows = rowsOfKind(rows[0].Rows, "source")
	if len(sourceRows) != 1 || sourceRows[0].State != "stale" || sourceRows[0].Remediation == "" {
		t.Fatalf("stale row = %+v", sourceRows)
	}
	if !rows[0].NeedsAttention {
		t.Fatal("stale source should be attention")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
