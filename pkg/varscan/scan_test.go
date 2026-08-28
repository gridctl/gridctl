package varscan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestScan_WorkingTreeRedactsAndScopesLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.txt")
	secret := "long-secret-value"
	if err := os.WriteFile(path, []byte("before "+secret+" after\nlong-secret\n-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), []vault.Variable{{Key: "TOKEN", Value: secret, IsSecret: true}}, Options{Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings=%d", len(result.Findings))
	}
	if strings.Contains(result.Findings[0].Snippet, secret) {
		t.Fatal("snippet leaked secret")
	}
	if result.Findings[0].Column != 8 {
		t.Fatalf("column=%d", result.Findings[0].Column)
	}
}

func TestScan_SkipsShortEmptyAndBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary")
	if err := os.WriteFile(path, []byte("abc\x00long-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), []vault.Variable{{Key: "EMPTY", IsSecret: true}, {Key: "SHORT", Value: "short", IsSecret: true}, {Key: "LONG", Value: "long-secret", IsSecret: true}}, Options{Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Skipped) != 3 {
		t.Fatalf("skipped=%#v", result.Skipped)
	}
}

func TestScan_StagedReadsIndexInsteadOfWorkingTree(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.txt")
	if err := os.WriteFile(path, []byte("safe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("config.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("base", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.invalid", When: time.Unix(1, 0)}}); err != nil {
		t.Fatal(err)
	}
	secret := "staged-secret-value"
	if err := os.WriteFile(path, []byte(secret+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("config.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("safe again\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	vars := []vault.Variable{{Key: "TOKEN", Value: secret, IsSecret: true}}
	staged, err := Scan(context.Background(), vars, Options{Staged: true})
	if err != nil {
		t.Fatal(err)
	}
	working, err := Scan(context.Background(), vars, Options{Paths: []string{"config.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(staged.Findings) != 1 || len(working.Findings) != 0 {
		t.Fatalf("staged=%#v working=%#v", staged.Findings, working.Findings)
	}
}

func TestScan_StagedIncludesAddedRenamedAndIndexedIgnoredFiles(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("safe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("old.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("base", &git.CommitOptions{Author: &object.Signature{Name: "Test", Email: "test@example.invalid", When: time.Unix(1, 0)}}); err != nil {
		t.Fatal(err)
	}
	secret := "staged-secret-value"
	if err := os.Rename(oldPath, filepath.Join(dir, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Remove("old.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "renamed.txt"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("renamed.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "added.txt"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("added.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "forced.txt"), []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("forced.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("forced.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add(".gitignore"); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	result, err := Scan(context.Background(), []vault.Variable{{Key: "TOKEN", Value: secret, IsSecret: true}}, Options{Staged: true})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]bool{}
	for _, finding := range result.Findings {
		files[finding.File] = true
	}
	for _, want := range []string{"added.txt", "renamed.txt", "forced.txt"} {
		if !files[want] {
			t.Fatalf("missing %s in findings %#v", want, result.Findings)
		}
	}
}

func TestScan_WorkingTreeUsesRequestedRepositoryGitignore(t *testing.T) {
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	secret := "ignored-secret-value"
	ignored := filepath.Join(dir, "ignored.txt")
	if err := os.WriteFile(ignored, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), []vault.Variable{{Key: "TOKEN", Value: secret, IsSecret: true}}, Options{Paths: []string{ignored}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 || len(result.Skipped) != 1 || result.Skipped[0].Reason != "ignored" {
		t.Fatalf("result = %+v", result)
	}
}

func TestScan_SkipsLargeFilesAndSymlinks(t *testing.T) {
	dir := t.TempDir()
	large := filepath.Join(dir, "large.txt")
	f, err := os.Create(large)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFileSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("safe"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), nil, Options{Paths: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]bool{}
	for _, skip := range result.Skipped {
		reasons[skip.Reason] = true
	}
	if !reasons["file exceeds 10 MiB"] || !reasons["symlink"] {
		t.Fatalf("skips = %#v", result.Skipped)
	}
}

func TestScan_CapsFindingsPerFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "many.txt")
	secret := "repeated-secret-value"
	if err := os.WriteFile(path, []byte(strings.Repeat(secret+"\n", MaxFindingsPerFile+1)), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), []vault.Variable{{Key: "TOKEN", Value: secret, IsSecret: true}}, Options{Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != MaxFindingsPerFile || len(result.Truncations) != 1 {
		t.Fatalf("findings=%d truncations=%#v", len(result.Findings), result.Truncations)
	}
}

func TestScan_OverlapsAreLongestFirstAndDeterministic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overlap.txt")
	if err := os.WriteFile(path, []byte("long-secret-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	vars := []vault.Variable{
		{Key: "SHORT", Value: "long-secret", IsSecret: true},
		{Key: "LONG", Value: "long-secret-value", IsSecret: true},
	}
	first, err := Scan(context.Background(), vars, Options{Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Scan(context.Background(), vars, Options{Paths: []string{path}})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Findings) != 2 || first.Findings[0].Key != "LONG" {
		t.Fatalf("findings = %#v", first.Findings)
	}
	if strings.Contains(first.Findings[0].Snippet, "long-secret") || fmt.Sprintf("%#v", first) != fmt.Sprintf("%#v", second) {
		t.Fatalf("nondeterministic or unsafe results: first=%#v second=%#v", first, second)
	}
}
