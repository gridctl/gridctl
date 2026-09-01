package builder

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type stubPyPIResolver struct {
	release *PyPIRelease
	err     error
}

func (s stubPyPIResolver) Resolve(context.Context, string, string, string) (*PyPIRelease, error) {
	return s.release, s.err
}

func TestResolve_LocalContentDeterminesIdentity(t *testing.T) {
	dir := t.TempDir()
	dockerfile := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dockerfile, []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b := New(&mockDockerClient{})
	opts := BuildOptions{Stack: "demo", ServerName: "fetch", SourceType: "local", Path: dir,
		BuildArgs: map[string]string{"MODE": "prod"}, Command: []string{"serve"}, Platform: "linux/amd64"}

	first, err := b.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	defer first.Close()
	second, err := b.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatalf("Resolve repeat: %v", err)
	}
	defer second.Close()
	if first.BuildInputDigest != second.BuildInputDigest || first.ImageTag != second.ImageTag {
		t.Fatal("unchanged inputs produced different identities")
	}
	if strings.Contains(first.ImageTag, ":latest") {
		t.Fatalf("image tag is mutable: %s", first.ImageTag)
	}

	if err := os.WriteFile(dockerfile, []byte("FROM alpine:3.20\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := b.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatalf("Resolve changed: %v", err)
	}
	defer changed.Close()
	if changed.BuildInputDigest == first.BuildInputDigest || changed.ImageTag == first.ImageTag {
		t.Fatal("source content change did not invalidate image identity")
	}
}

func TestResolve_BuildInputsInvalidateIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b := New(&mockDockerClient{})
	base := BuildOptions{Stack: "demo", ServerName: "server", SourceType: "local", Path: dir}
	first, err := b.Resolve(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	changes := []BuildOptions{
		{Stack: "demo", ServerName: "server", SourceType: "local", Path: dir, BuildArgs: map[string]string{"A": "1"}},
		{Stack: "demo", ServerName: "server", SourceType: "local", Path: dir, Command: []string{"other"}},
		{Stack: "demo", ServerName: "server", SourceType: "local", Path: dir, Platform: "linux/arm64"},
	}
	for i, opts := range changes {
		plan, err := b.Resolve(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if plan.BuildInputDigest == first.BuildInputDigest {
			t.Errorf("change %d did not invalidate digest", i)
		}
		_ = plan.Close()
	}
}

func TestResolve_EmptyBuildInputsHaveCanonicalIdentity(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	b := New(&mockDockerClient{})
	omitted, err := b.Resolve(context.Background(), BuildOptions{
		Stack: "demo", ServerName: "server", SourceType: "local", Path: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer omitted.Close()
	empty, err := b.Resolve(context.Background(), BuildOptions{
		Stack: "demo", ServerName: "server", SourceType: "local", Path: dir,
		BuildArgs: map[string]string{}, Command: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer empty.Close()
	if omitted.BuildInputDigest != empty.BuildInputDigest || omitted.ImageTag != empty.ImageTag {
		t.Fatal("omitted and empty build inputs produced different identities")
	}
}

func TestResolve_PyPIGeneratesImmutablePythonPlan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := New(&mockDockerClient{})
	b.pypiResolver = stubPyPIResolver{release: &PyPIRelease{
		Package: "mcp-demo", Version: "1.2.3", Python: "3.12",
		Artifact: PyPIArtifact{Filename: "mcp_demo-1.2.3-py3-none-any.whl", SHA256: strings.Repeat("a", 64)},
		Metadata: PythonPackageMetadata{Name: "mcp-demo", Version: "1.2.3", ConsoleScripts: []string{"mcp-demo"}},
	}}
	plan, err := b.Resolve(context.Background(), BuildOptions{
		Stack: "demo", ServerName: "package", SourceType: "pypi", Package: "mcp-demo", Ref: "1.2.3",
		Extras: []string{"http"}, With: []string{"httpx>=0.27"}, Packages: []string{"curl"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if plan.Python != "3.12" || plan.Command[0] != "mcp-demo" || plan.GeneratedDockerfile == "" {
		t.Fatalf("resolved plan = %+v", plan)
	}
	if plan.ResolvedIdentity.ArtifactSHA256 == "" || !strings.Contains(plan.ImageTag, ":1.2.3-") {
		t.Fatalf("PyPI identity = %+v, tag = %s", plan.ResolvedIdentity, plan.ImageTag)
	}
	if plan.Provenance.GeneratorVersion != PythonTemplateVersion || plan.Provenance.BaseImage == "" || plan.Provenance.UVImage == "" {
		t.Fatalf("Python provenance = %+v", plan.Provenance)
	}
	builtDockerfile, err := os.ReadFile(filepath.Join(plan.EffectiveProjectRoot, plan.Dockerfile))
	if err != nil || string(builtDockerfile) != plan.GeneratedDockerfile {
		t.Fatalf("built Dockerfile differs from preview: %v", err)
	}
}

func TestResolve_LocalPythonUsesSubprojectAndIgnoresDefaultDockerfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM should-not-win\n"), 0644); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "servers", "demo")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	metadata := "[project]\nname = \"demo\"\nversion = \"1.0\"\nrequires-python = \">=3.11\"\n[project.scripts]\ndemo = \"demo:main\"\n"
	if err := os.WriteFile(filepath.Join(project, "pyproject.toml"), []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}
	plan, err := New(&mockDockerClient{}).Resolve(context.Background(), BuildOptions{
		Stack: "demo", ServerName: "local", SourceType: "local", Path: root,
		ProjectPath: "servers/demo", Runtime: "python", Extras: []string{"cli"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if plan.Python != "3.11" || plan.Command[0] != "demo" || !strings.Contains(plan.GeneratedDockerfile, "uv tool install '/app[cli]'") {
		t.Fatalf("generated local plan = %+v\n%s", plan, plan.GeneratedDockerfile)
	}
	if _, err := os.Stat(filepath.Join(project, generatedDockerfileName)); !os.IsNotExist(err) {
		t.Fatalf("generated file modified user source: %v", err)
	}
}

func TestResolve_PythonExplicitDockerfileTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Containerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	plan, err := New(&mockDockerClient{}).Resolve(context.Background(), BuildOptions{
		Stack: "demo", ServerName: "custom", SourceType: "local", Path: dir,
		Runtime: "python", Dockerfile: "Containerfile", Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if plan.GeneratedDockerfile != "" || plan.Dockerfile != "Containerfile" {
		t.Fatalf("explicit Dockerfile plan = %+v", plan)
	}
	if !strings.Contains(logs.String(), "Found configured Dockerfile; building from it.") {
		t.Fatalf("precedence log missing: %s", logs.String())
	}
}

func TestResolve_LocalPythonRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "project")); err != nil {
		t.Fatal(err)
	}
	_, err := New(&mockDockerClient{}).Resolve(context.Background(), BuildOptions{
		SourceType: "local", Path: root, ProjectPath: "project", Runtime: "python",
	})
	if err == nil || !strings.Contains(err.Error(), "escapes the source root") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestGenerateImageTag_SanitizationCannotAlias(t *testing.T) {
	first := GenerateImageTag("demo", "a b", "v1", strings.Repeat("a", 64))
	second := GenerateImageTag("demo", "a-b", "v1", strings.Repeat("a", 64))
	if first == second {
		t.Fatalf("sanitized names collided: %s", first)
	}
	if strings.ContainsAny(first, " @") {
		t.Fatalf("tag contains invalid characters: %s", first)
	}
	caseVariant := GenerateImageTag("demo", "A-B", "v1", strings.Repeat("a", 64))
	if caseVariant == second {
		t.Fatalf("case normalization collided: %s", caseVariant)
	}
	long := GenerateImageTag(strings.Repeat("s", 300), strings.Repeat("n", 300), strings.Repeat("p", 300), "short")
	if len(long) > 255 {
		t.Fatalf("image reference exceeds OCI length: %d", len(long))
	}
}

func TestResolvedBuildPlan_CloseRemovesOwnedWorktree(t *testing.T) {
	dir := t.TempDir()
	plan := &ResolvedBuildPlan{EffectiveProjectRoot: dir, cleanup: func() error { return os.RemoveAll(dir) }}
	if err := plan.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
	if err := plan.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestResolvedBuildPlan_ImageLabelsExcludeCredentials(t *testing.T) {
	plan := &ResolvedBuildPlan{
		DeclaredIdentity: SourceIdentity{URL: "https://secret@github.com/example/repo?token=hidden"},
		ResolvedIdentity: SourceIdentity{Commit: strings.Repeat("a", 40)},
		BuildInputDigest: strings.Repeat("b", 64),
		Provenance:       BuildProvenance{SourceContentDigest: strings.Repeat("c", 64), GeneratorVersion: PythonTemplateVersion},
	}
	labels := plan.ImageLabels()
	if labels[LabelBuildInputDigest] != plan.BuildInputDigest || labels["org.opencontainers.image.revision"] != plan.ResolvedIdentity.Commit || labels[LabelGeneratorVersion] != PythonTemplateVersion {
		t.Fatalf("labels = %v", labels)
	}
	if labels["org.opencontainers.image.source"] != "https://github.com/example/repo" {
		t.Fatalf("source label leaked URL credentials: %q", labels["org.opencontainers.image.source"])
	}
	for key, value := range labels {
		if strings.Contains(strings.ToLower(key+value), "credential") || strings.Contains(value, "token") {
			t.Fatalf("sensitive label %s=%s", key, value)
		}
	}
}

func TestResolve_GitUsesFreshRefAndIsolatedWorktrees(t *testing.T) {
	remote := t.TempDir()
	repo, err := gogit.PlainInit(remote, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(
		plumbing.HEAD, plumbing.NewBranchReferenceName("main"),
	)); err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commit := func(content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(remote, "Dockerfile"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add("Dockerfile"); err != nil {
			t.Fatal(err)
		}
		hash, err := wt.Commit("update", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.com"}})
		if err != nil {
			t.Fatal(err)
		}
		return hash.String()
	}

	firstSHA := commit("FROM alpine\n")
	if _, err := repo.CreateTag("v1.0.0", plumbing.NewHash(firstSHA), nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	b := New(&mockDockerClient{})
	opts := BuildOptions{Stack: "demo", ServerName: "git", SourceType: "git", URL: remote}
	first, err := b.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	defer first.Close()
	if first.ResolvedIdentity.Commit != firstSHA {
		t.Fatalf("first commit = %s, want %s", first.ResolvedIdentity.Commit, firstSHA)
	}
	if first.DeclaredIdentity.Ref != "" || first.ResolvedIdentity.Ref != "main" {
		t.Fatalf("declared ref = %q, resolved ref = %q", first.DeclaredIdentity.Ref, first.ResolvedIdentity.Ref)
	}
	if !first.MutableRef {
		t.Fatal("branch ref was not reported as mutable")
	}

	unchanged, err := b.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatalf("unchanged Resolve: %v", err)
	}
	defer unchanged.Close()
	if unchanged.EffectiveProjectRoot == first.EffectiveProjectRoot {
		t.Fatal("unchanged resolves share a mutable worktree")
	}
	if unchanged.BuildInputDigest != first.BuildInputDigest || unchanged.ImageTag != first.ImageTag {
		t.Fatal("isolated worktrees at the same commit produced different identities")
	}
	for _, ref := range []string{"v1.0.0", firstSHA} {
		pinnedOpts := opts
		pinnedOpts.Ref = ref
		pinned, err := b.Resolve(context.Background(), pinnedOpts)
		if err != nil {
			t.Fatalf("Resolve pinned ref %q: %v", ref, err)
		}
		if pinned.MutableRef {
			t.Errorf("pinned ref %q was reported as mutable", ref)
		}
		if pinned.ResolvedIdentity.Commit != firstSHA {
			t.Errorf("pinned ref %q resolved to %s, want %s", ref, pinned.ResolvedIdentity.Commit, firstSHA)
		}
		_ = pinned.Close()
	}

	secondSHA := commit("FROM alpine:3.20\n")
	second, err := b.Resolve(context.Background(), opts)
	if err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	defer second.Close()
	if second.ResolvedIdentity.Commit != secondSHA {
		t.Fatalf("second commit = %s, want %s", second.ResolvedIdentity.Commit, secondSHA)
	}
	if first.EffectiveProjectRoot == second.EffectiveProjectRoot {
		t.Fatal("active build plans share a mutable worktree")
	}
	firstDockerfile, err := os.ReadFile(filepath.Join(first.EffectiveProjectRoot, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(firstDockerfile) != "FROM alpine\n" {
		t.Fatalf("first worktree mutated after second resolve: %q", firstDockerfile)
	}
}

func TestResolve_GitPythonUsesCheckoutSubproject(t *testing.T) {
	remote := t.TempDir()
	repo, err := gogit.PlainInit(remote, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(remote, "services", "demo")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatal(err)
	}
	metadata := "[project]\nname = \"git-demo\"\nversion = \"2.0\"\n[project.scripts]\ngit-demo = \"demo:main\"\n"
	if err := os.WriteFile(filepath.Join(project, "pyproject.toml"), []byte(metadata), 0644); err != nil {
		t.Fatal(err)
	}
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("services/demo/pyproject.toml"); err != nil {
		t.Fatal(err)
	}
	commit, err := worktree.Commit("python project", &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	plan, err := New(&mockDockerClient{}).Resolve(context.Background(), BuildOptions{
		Stack: "demo", ServerName: "git-python", SourceType: "git", URL: remote,
		Ref: commit.String(), Path: "services/demo", Runtime: "python",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if plan.ResolvedIdentity.Commit != commit.String() || plan.ResolvedIdentity.Path != "services/demo" {
		t.Fatalf("resolved git identity = %+v", plan.ResolvedIdentity)
	}
	if plan.Command[0] != "git-demo" || plan.GeneratedDockerfile == "" {
		t.Fatalf("generated git plan = %+v", plan)
	}
	if _, err := os.Stat(filepath.Join(plan.EffectiveProjectRoot, "pyproject.toml")); err != nil {
		t.Fatalf("selected subproject was not materialized: %v", err)
	}
	if _, err := os.Stat(filepath.Join(plan.EffectiveProjectRoot, "services")); !os.IsNotExist(err) {
		t.Fatalf("clone root was copied instead of selected subproject: %v", err)
	}
}
