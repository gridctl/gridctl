package git

import (
	"os"
	"path/filepath"
	"testing"

	gogit "github.com/go-git/go-git/v5"
)

func TestIsRepoRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := gogit.PlainInit(root, false); err != nil {
		t.Fatalf("git init: %v", err)
	}
	ok, err := IsRepoRoot(root)
	if err != nil || !ok {
		t.Fatalf("IsRepoRoot(root) = %v, %v", ok, err)
	}

	child := filepath.Join(root, "skill")
	if err := os.Mkdir(child, 0o755); err != nil {
		t.Fatal(err)
	}
	ok, err = IsRepoRoot(child)
	if err != nil || ok {
		t.Fatalf("IsRepoRoot(child) = %v, %v; want false, nil", ok, err)
	}

	plain := t.TempDir()
	ok, err = IsRepoRoot(plain)
	if err != nil || ok {
		t.Fatalf("IsRepoRoot(plain) = %v, %v; want false, nil", ok, err)
	}
}
