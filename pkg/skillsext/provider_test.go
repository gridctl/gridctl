package skillsext

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

func fixture(t *testing.T) (*registry.Store, *Provider) {
	t.Helper()
	store := registry.NewStore(t.TempDir())
	if err := store.SaveSkill(&registry.AgentSkill{Name: "alpha", Description: "Alpha skill", State: registry.StateActive,
		AcceptanceCriteria: []string{"Given a task, finish it"}, Extra: map[string]any{"custom": "value"}, Body: "Body\n"}); err != nil {
		t.Fatal(err)
	}
	return store, New(store)
}

func write(t *testing.T, store *registry.Store, file string, data []byte) {
	t.Helper()
	if err := store.WriteFile("alpha", file, data); err != nil {
		t.Fatal(err)
	}
}

func TestProvider_ManifestAndReads(t *testing.T) {
	store, provider := fixture(t)
	for file, data := range map[string][]byte{
		"helper.py": []byte("print('hello')"), "scripts/run.sh": []byte("#!/bin/sh\n"),
		"references/notes.md": []byte("Notes"), "assets/logo.png": {0x89, 'P', 'N', 'G', 0},
		".hidden": []byte("private"), "notes.tmp": []byte("temporary"),
		"child/SKILL.md": []byte("---\nname: child\ndescription: child\n---\n"), "child/notes.md": []byte("child notes"),
	} {
		write(t, store, file, data)
	}
	if err := os.Symlink(store.Dir(), filepath.Join(store.Dir(), "skills", "alpha", "scripts", "link")); err != nil {
		t.Fatal(err)
	}
	entries, err := provider.ListSkillEntries(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].Resources) != 5 {
		t.Fatalf("entries: %+v", entries)
	}
	entry := entries[0]
	var frontmatter map[string]any
	if err := json.Unmarshal(entry.Frontmatter, &frontmatter); err != nil {
		t.Fatal(err)
	}
	if frontmatter["name"] != "alpha" || frontmatter["description"] != "Alpha skill" || frontmatter["custom"] != "value" || frontmatter["acceptance_criteria"] == nil {
		t.Fatalf("frontmatter = %s", entry.Frontmatter)
	}
	if _, ok := frontmatter["state"]; ok {
		t.Fatal("state leaked")
	}
	for _, f := range entry.Resources {
		file := strings.TrimPrefix(f.URI, "skill://registry/alpha/")
		content, err := provider.ReadSkillFile(t.Context(), "alpha", file)
		if err != nil {
			t.Fatal(err)
		}
		data := []byte(content.Text)
		if file == "assets/logo.png" {
			if content.Blob == nil || content.MimeType != "image/png" {
				t.Fatalf("binary content: %+v", content)
			}
			data = content.Blob
		}
		sum := sha256.Sum256(data)
		if f.Digest != "sha256:"+hex.EncodeToString(sum[:]) || f.Size != int64(len(data)) {
			t.Fatalf("content disagrees with %+v", f)
		}
		if file == "SKILL.md" {
			fm, err := frontmatterJSON(data)
			if err != nil || !bytes.Equal(fm, entry.Frontmatter) {
				t.Fatalf("frontmatter differs: %s, %v", fm, err)
			}
		}
	}
	for _, file := range []string{".hidden", "notes.tmp", "child/SKILL.md", "child/notes.md", "scripts/link", "../alpha/SKILL.md"} {
		if _, err := provider.ReadSkillFile(t.Context(), "alpha", file); !errors.Is(err, mcp.ErrSkillNotFound) {
			t.Errorf("read %q: %v", file, err)
		}
	}
	children, err := provider.ReadSkillDirectory(t.Context(), "alpha", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(children) != 5 {
		t.Fatalf("children = %+v", children)
	}
	for _, child := range children {
		if child.Name == "alpha" && (child.Description != "Alpha skill" || child.MimeType != "text/markdown") {
			t.Fatalf("skill child = %+v", child)
		}
		if child.Name == "assets" && child.MimeType != "inode/directory" {
			t.Fatalf("directory child = %+v", child)
		}
	}
	scripts, err := provider.ReadSkillDirectory(t.Context(), "alpha", "scripts")
	if err != nil || len(scripts) != 1 || scripts[0].Name != "run.sh" {
		t.Fatalf("scripts = %+v, %v", scripts, err)
	}
	for _, dir := range []string{"child", ".hidden", "SKILL.md", "scripts/", "absent"} {
		if _, err := provider.ReadSkillDirectory(t.Context(), "alpha", dir); !errors.Is(err, mcp.ErrSkillNotFound) {
			t.Errorf("directory %q: %v", dir, err)
		}
	}
	sk, err := store.GetSkill("alpha")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := skillpins.ComputeManifest(t.Context(), store, sk)
	if err != nil {
		t.Fatal(err)
	}
	hash, files, err := skillpins.ComputeDigests(store, sk)
	if err != nil {
		t.Fatal(err)
	}
	if Summarize(manifest).Digest != skillpins.CompositeHash(hash, files) {
		t.Fatal("summary digest differs from pins")
	}
}

func TestProvider_GenerationCache(t *testing.T) {
	store, provider := fixture(t)
	write(t, store, "helper.txt", []byte("old"))
	first, err := provider.GetSkillEntry(t.Context(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Dir(), "skills", "alpha", "helper.txt"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	cached, err := provider.GetSkillEntry(t.Context(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, cached) {
		t.Fatal("cache changed without a refresh")
	}
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	fresh, err := provider.GetSkillEntry(t.Context(), "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Resources[0] != first.Resources[0] || fresh.Resources[1].Digest == first.Resources[1].Digest {
		t.Fatal("refresh did not isolate changed file")
	}
	fresh.Resources[0].Digest = "corrupted by caller"
	if got, err := provider.GetSkillEntry(t.Context(), "alpha"); err != nil || got.Resources[0].Digest != first.Resources[0].Digest {
		t.Fatal("caller mutated cached manifest")
	}
	sk, err := store.GetSkill("alpha")
	if err != nil {
		t.Fatal(err)
	}
	sk.State = registry.StateDisabled
	if err := store.SaveSkill(sk); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetSkillEntry(t.Context(), "alpha"); !errors.Is(err, mcp.ErrSkillNotFound) {
		t.Fatalf("disabled get: %v", err)
	}
	entries, err := provider.ListSkillEntries(t.Context())
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatalf("disabled list: %+v, %v", entries, err)
	}
}

func TestProvider_DynamicLimits(t *testing.T) {
	for _, kind := range []string{"files", "bytes"} {
		t.Run(kind, func(t *testing.T) {
			store, provider := fixture(t)
			if kind == "files" {
				for i := 0; i < 512; i++ {
					write(t, store, fmt.Sprintf("file-%03d.txt", i), []byte("content"))
				}
			} else {
				write(t, store, "large.txt", bytes.Repeat([]byte("x"), MaxManifestBytes))
			}
			var logs bytes.Buffer
			old := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(old) })
			for i := 0; i < 2; i++ {
				entries, err := provider.ListSkillEntries(t.Context())
				if err != nil || len(entries) != 1 || !entries[0].Dynamic {
					t.Fatalf("dynamic entries = %+v, %v", entries, err)
				}
				data, err := json.Marshal(entries[0])
				if err != nil || !bytes.Contains(data, []byte(`"resources":"dynamic"`)) {
					t.Fatalf("dynamic JSON = %s, %v", data, err)
				}
			}
			file := "file-000.txt"
			if kind == "bytes" {
				file = "large.txt"
			}
			if _, err := provider.ReadSkillFile(t.Context(), "alpha", file); err != nil {
				t.Fatal(err)
			}
			if strings.Count(logs.String(), "level=WARN") != 1 {
				t.Fatalf("warnings: %s", logs.String())
			}
			if err := store.Load(); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.GetSkillEntry(t.Context(), "alpha"); err != nil {
				t.Fatal(err)
			}
			if strings.Count(logs.String(), "level=WARN") != 2 {
				t.Fatalf("generation warning: %s", logs.String())
			}
		})
	}
}

func TestProvider_RejectsReplacedLink(t *testing.T) {
	store, provider := fixture(t)
	write(t, store, "helper.txt", []byte("safe"))
	if _, err := provider.GetSkillEntry(t.Context(), "alpha"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(store.Dir(), "skills", "alpha", "helper.txt")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(store.Dir(), "outside"), file); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.ReadSkillFile(t.Context(), "alpha", "helper.txt"); !errors.Is(err, mcp.ErrSkillNotFound) {
		t.Fatalf("replaced link: %v", err)
	}
}

func TestProvider_CancellationAndFrontmatterErrors(t *testing.T) {
	_, provider := fixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, call := range []func() error{
		func() error { _, err := provider.ListSkillEntries(ctx); return err },
		func() error { _, err := provider.GetSkillEntry(ctx, "alpha"); return err },
		func() error { _, err := provider.ReadSkillFile(ctx, "alpha", "SKILL.md"); return err },
		func() error { _, err := provider.ReadSkillDirectory(ctx, "alpha", ""); return err },
	} {
		if err := call(); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled operation: %v", err)
		}
	}
	for _, data := range []string{"body only", "---\nname: alpha\n", "---\nname: alpha\nextra:\n  1: value\n---\nbody"} {
		if _, err := frontmatterJSON([]byte(data)); err == nil {
			t.Fatalf("accepted invalid JSON frontmatter: %s", data)
		}
	}
}
