package skillpins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

// newTestRegistry writes skills into a temp registry tree and loads a real
// registry.Store over it, so hashing exercises the production parse path.
func newTestRegistry(t *testing.T) *registry.Store {
	t.Helper()
	store := registry.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("loading empty registry: %v", err)
	}
	return store
}

func writeSkill(t *testing.T, store *registry.Store, name, content string) {
	t.Helper()
	dir := filepath.Join(store.Dir(), "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing SKILL.md: %v", err)
	}
	if err := store.Load(); err != nil {
		t.Fatalf("reloading registry: %v", err)
	}
}

func writeSupportingFile(t *testing.T, store *registry.Store, skill, rel string, content []byte) {
	t.Helper()
	path := filepath.Join(store.Dir(), "skills", skill, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating supporting dir: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("writing supporting file: %v", err)
	}
}

const fixtureSkill = `---
name: incident-triage
description: Walk an incident from page to postmortem.
license: MIT
metadata:
  team: sre
argument-hint: "<incident-id>"
---

# Incident triage

Start with the pager context, then check the dashboards.
`

// TestCanonicalSkillHash_RoundTripStable is the regression the whole feature
// rests on: a parse -> render -> parse round trip must not move the hash, or
// frontmatter normalization on import/save would manufacture pin drift for
// every skill.
func TestCanonicalSkillHash_RoundTripStable(t *testing.T) {
	sk, err := registry.ParseSkillMD([]byte(fixtureSkill))
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	h1, err := CanonicalSkillHash(sk)
	if err != nil {
		t.Fatalf("hashing parsed skill: %v", err)
	}

	rendered, err := registry.RenderSkillMD(sk)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	reparsed, err := registry.ParseSkillMD(rendered)
	if err != nil {
		t.Fatalf("reparsing rendered skill: %v", err)
	}
	h2, err := CanonicalSkillHash(reparsed)
	if err != nil {
		t.Fatalf("hashing reparsed skill: %v", err)
	}

	if h1 != h2 {
		t.Fatalf("canonical hash moved across a parse/render round trip:\n  first:  %s\n  second: %s", h1, h2)
	}
}

// TestCanonicalSkillHash_NormalizationInvariant: the raw on-disk form and its
// canonical rendering hash identically after parsing, even when the raw file
// uses CRLF, omits state, and orders extras differently from the renderer.
func TestCanonicalSkillHash_NormalizationInvariant(t *testing.T) {
	raw := "---\r\nzeta-extra: 1\r\nname: n\r\ndescription: d\r\n---\r\n\r\nBody line.\r\n"
	sk, err := registry.ParseSkillMD([]byte(raw))
	if err != nil {
		t.Fatalf("parsing raw: %v", err)
	}
	h1, err := CanonicalSkillHash(sk)
	if err != nil {
		t.Fatalf("hashing raw parse: %v", err)
	}

	canonical, err := registry.RenderSkillMD(sk)
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	sk2, err := registry.ParseSkillMD(canonical)
	if err != nil {
		t.Fatalf("parsing canonical: %v", err)
	}
	h2, err := CanonicalSkillHash(sk2)
	if err != nil {
		t.Fatalf("hashing canonical parse: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("normalized forms hash differently: %s vs %s", h1, h2)
	}
}

func TestCanonicalSkillHash_BodyChangeMoves(t *testing.T) {
	sk, err := registry.ParseSkillMD([]byte(fixtureSkill))
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	h1, err := CanonicalSkillHash(sk)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	sk.Body += "\nNew paragraph.\n"
	h2, err := CanonicalSkillHash(sk)
	if err != nil {
		t.Fatalf("hashing changed body: %v", err)
	}
	if h1 == h2 {
		t.Fatal("body change did not move the canonical hash")
	}
}

func TestComputeDigests_FiltersNonContent(t *testing.T) {
	store := newTestRegistry(t)
	writeSkill(t, store, "demo", fixtureSkill)
	writeSupportingFile(t, store, "demo", "scripts/run.sh", []byte("#!/bin/sh\n"))
	writeSupportingFile(t, store, "demo", ".origin.json", []byte(`{"repo":"r"}`))
	writeSupportingFile(t, store, "demo", "references/.hidden", []byte("x"))
	writeSupportingFile(t, store, "demo", "notes.tmp", []byte("t"))

	// The registry registers skills under the directory name (it wins over
	// frontmatter), so the fixture loads as "demo".
	sk, err := store.GetSkill("demo")
	if err != nil {
		t.Fatalf("get skill: %v", err)
	}
	_, files, err := ComputeDigests(store, sk)
	if err != nil {
		t.Fatalf("computing digests: %v", err)
	}
	if len(files) != 1 || files[0].Path != "scripts/run.sh" {
		t.Fatalf("digest set = %+v, want exactly scripts/run.sh", files)
	}
}

// TestCanonicalSkillHash_StateExcluded: the gridctl-managed state field is
// exposure metadata, not content; toggling it must not move the hash.
func TestCanonicalSkillHash_StateExcluded(t *testing.T) {
	sk, err := registry.ParseSkillMD([]byte(fixtureSkill))
	if err != nil {
		t.Fatal(err)
	}
	sk.State = registry.StateActive
	h1, err := CanonicalSkillHash(sk)
	if err != nil {
		t.Fatal(err)
	}
	sk.State = registry.StateDisabled
	h2, err := CanonicalSkillHash(sk)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Fatal("state toggle moved the canonical hash")
	}
}

// TestComputeDigests_ExcludesNestedSkillTrees: a subdirectory holding its
// own SKILL.md is a separate registered skill; its tree must not
// contaminate the parent's digest set.
func TestComputeDigests_ExcludesNestedSkillTrees(t *testing.T) {
	store := newTestRegistry(t)
	writeSkill(t, store, "parent", fixtureSkill)
	writeSupportingFile(t, store, "parent", "scripts/run.sh", []byte("#!/bin/sh\n"))
	writeSupportingFile(t, store, "parent", "child/SKILL.md", []byte("---\nname: child\ndescription: d\n---\n\nChild body.\n"))
	writeSupportingFile(t, store, "parent", "child/notes.md", []byte("child notes"))

	sk, err := store.GetSkill("parent")
	if err != nil {
		t.Fatal(err)
	}
	_, files, err := ComputeDigests(store, sk)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "scripts/run.sh" {
		t.Fatalf("digest set = %+v, want only scripts/run.sh (nested skill excluded)", files)
	}
}

func TestCompositeHash_OrderIndependentInputSensitive(t *testing.T) {
	files := []FileDigest{{Path: "a", Digest: "s1:1"}, {Path: "b", Digest: "s1:2"}}
	h1 := CompositeHash("s1:doc", files)
	if h2 := CompositeHash("s1:doc", files); h2 != h1 {
		t.Fatal("composite hash is not deterministic")
	}
	if h2 := CompositeHash("s1:doc2", files); h2 == h1 {
		t.Fatal("composite hash ignored the skill hash")
	}
	if h2 := CompositeHash("s1:doc", files[:1]); h2 == h1 {
		t.Fatal("composite hash ignored the file set")
	}
}

func TestDiffFileSets(t *testing.T) {
	pinned := []FileDigest{{Path: "a", Digest: "1"}, {Path: "b", Digest: "2"}, {Path: "c", Digest: "3"}}
	current := []FileDigest{{Path: "b", Digest: "2"}, {Path: "c", Digest: "9"}, {Path: "d", Digest: "4"}}
	added, removed, modified := diffFileSets(pinned, current)
	if len(added) != 1 || added[0] != "d" {
		t.Fatalf("added = %v, want [d]", added)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Fatalf("removed = %v, want [a]", removed)
	}
	if len(modified) != 1 || modified[0] != "c" {
		t.Fatalf("modified = %v, want [c]", modified)
	}
}
