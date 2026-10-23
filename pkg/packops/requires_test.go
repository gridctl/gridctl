package packops

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/skills"
	"gopkg.in/yaml.v3"
)

func TestEnforcePackRequires_SyntaxNamesPack(t *testing.T) {
	config.SetRunningVersion("1.0.0")
	t.Cleanup(config.ResetRunningVersion)
	err := enforcePackRequires("netops", "not a constraint")
	if err == nil || !errors.Is(err, ErrPackRequires) {
		t.Fatalf("err = %v", err)
	}
	if err.Error() != `pack netops: requires.gridctl: must be a semver constraint (e.g. ">=1.1.0")` {
		t.Fatalf("err = %q", err.Error())
	}
}

func TestAdd_UnsatisfiedRequiresImportsNothing(t *testing.T) {
	mgrs, imp := testEnv(t)
	config.SetRunningVersion("1.0.0")
	t.Cleanup(config.ResetRunningVersion)
	manifest := strings.Replace(testManifest, "wiring: false\n", "wiring: false\nrequires:\n  gridctl: \">=1.1.0\"\n", 1)
	repo := packFixture(t, manifest, nil)

	_, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo})
	if err == nil || err.Error() != "pack team-pack requires gridctl >=1.1.0, running 1.0.0" {
		t.Fatalf("Add err = %v", err)
	}
	if !errors.Is(err, ErrPackRequires) {
		t.Fatalf("errors.Is = false (%v)", err)
	}
	lf, err := skills.ReadLockFile(mgrs.lockPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := lf.FindPackSource("team-pack"); ok {
		t.Fatal("unsatisfied pack was imported")
	}
}

func TestAdd_SatisfiedRequiresIsRecorded(t *testing.T) {
	mgrs, imp := testEnv(t)
	config.SetRunningVersion("1.2.0")
	t.Cleanup(config.ResetRunningVersion)
	manifest := strings.Replace(testManifest, "wiring: false\n", "wiring: false\nrequires:\n  gridctl: \">=1.1.0\"\n", 1)
	repo := packFixture(t, manifest, nil)

	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	lf, err := skills.ReadLockFile(mgrs.lockPath())
	if err != nil {
		t.Fatal(err)
	}
	_, src, ok := lf.FindPackSource("team-pack")
	if !ok || src.Pack == nil || src.Pack.Requires != ">=1.1.0" {
		t.Fatalf("locked pack = %+v", src.Pack)
	}
	raw, err := os.ReadFile(mgrs.lockPath())
	if err != nil {
		t.Fatal(err)
	}
	var stamped struct {
		Version int `yaml:"version"`
	}
	if err := yaml.Unmarshal(raw, &stamped); err != nil {
		t.Fatal(err)
	}
	if stamped.Version != 2 {
		t.Fatalf("lock version = %d, want 2 (requires is additive)", stamped.Version)
	}
}

func TestAdd_DevBuildRecordsRequires(t *testing.T) {
	mgrs, imp := testEnv(t)
	config.SetRunningVersion("dev")
	t.Cleanup(config.ResetRunningVersion)
	manifest := strings.Replace(testManifest, "wiring: false\n", "wiring: false\nrequires:\n  gridctl: \">=9.0.0\"\n", 1)
	repo := packFixture(t, manifest, nil)
	if _, err := mgrs.Add(context.Background(), imp, AddOptions{Repo: repo}); err != nil {
		t.Fatal(err)
	}
	lf, err := skills.ReadLockFile(mgrs.lockPath())
	if err != nil {
		t.Fatal(err)
	}
	_, src, ok := lf.FindPackSource("team-pack")
	if !ok || src.Pack == nil || src.Pack.Requires != ">=9.0.0" {
		t.Fatalf("locked pack = %+v", src.Pack)
	}
}

func TestPreview_UnsatisfiedRequiresIsAWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config.SetRunningVersion("1.0.0")
	t.Cleanup(config.ResetRunningVersion)
	manifest := strings.Replace(testManifest, "name: team-pack", "name: netops", 1)
	manifest = strings.Replace(manifest, "wiring: false\n", "wiring: false\nrequires:\n  gridctl: \">=1.1.0\"\n", 1)
	repo := packFixture(t, manifest, nil)

	res, err := Preview(context.Background(), PreviewOptions{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if res.Pack != "netops" {
		t.Fatalf("preview aborted: %+v", res)
	}
	want := "pack netops requires gridctl >=1.1.0, running 1.0.0"
	found := false
	for _, warning := range res.Warnings {
		if warning == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v", res.Warnings)
	}
	lf, err := skills.ReadLockFile(skills.LockFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := lf.FindPackSource("netops"); ok {
		t.Fatal("preview wrote a pack record")
	}
}

func TestApply_UnsatisfiedRequiresProjectsNothing(t *testing.T) {
	mgrs, _ := testEnv(t)
	config.SetRunningVersion("1.0.0")
	t.Cleanup(config.ResetRunningVersion)
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"netops": {
			Repo: "https://example.com/netops.git",
			Pack: &skills.LockedPack{
				Name:     "netops",
				Requires: ">=1.1.0",
				Skills:   []string{"alpha"},
				Wiring:   true,
			},
		},
	}}
	if err := skills.WriteLockFile(mgrs.lockPath(), lf); err != nil {
		t.Fatal(err)
	}
	doc, err := mgrs.Apply(context.Background(), "netops", ApplyOptions{})
	if doc != nil {
		t.Fatalf("projected %+v", doc)
	}
	if err == nil || err.Error() != "pack netops requires gridctl >=1.1.0, running 1.0.0" {
		t.Fatalf("Apply err = %v", err)
	}
	if !errors.Is(err, ErrPackRequires) {
		t.Fatal("missing sentinel")
	}
}

func TestApply_MissingRequiresProceeds(t *testing.T) {
	mgrs, _ := testEnv(t)
	config.SetRunningVersion("1.0.0")
	t.Cleanup(config.ResetRunningVersion)
	lf := &skills.LockFile{Sources: map[string]skills.LockedSource{
		"netops": {
			Repo: "https://example.com/netops.git",
			Pack: &skills.LockedPack{Name: "netops"},
		},
	}}
	if err := skills.WriteLockFile(mgrs.lockPath(), lf); err != nil {
		t.Fatal(err)
	}
	doc, err := mgrs.Apply(context.Background(), "netops", ApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if doc == nil || doc.Pack != "netops" || len(doc.Rows) != 0 {
		t.Fatalf("doc = %+v", doc)
	}
}
