package builder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

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
