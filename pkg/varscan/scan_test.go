package varscan

import (
	"context"
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
