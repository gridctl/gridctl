package builder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir, err := CacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(".gridctl", "cache")) {
		t.Errorf("expected path ending in .gridctl/cache, got %q", dir)
	}
}

func TestReposCacheDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dir, err := ReposCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(".gridctl", "cache", "repos")) {
		t.Errorf("expected path ending in .gridctl/cache/repos, got %q", dir)
	}
}

func TestURLToPath_Deterministic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path1, err := URLToPath("https://github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	path2, err := URLToPath("https://github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path1 != path2 {
		t.Errorf("expected deterministic path, got %q and %q", path1, path2)
	}
}

func TestURLToPath_DifferentURLs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path1, err := URLToPath("https://github.com/org/repo-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	path2, err := URLToPath("https://github.com/org/repo-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path1 == path2 {
		t.Error("expected different paths for different URLs")
	}
}

func TestURLToPath_ContainsHash(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, err := URLToPath("https://github.com/org/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reposDir, err := ReposCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(path, reposDir) {
		t.Errorf("expected path under %q, got %q", reposDir, path)
	}

	// The basename should be a hex hash (16 chars from 8 bytes)
	base := filepath.Base(path)
	if len(base) != 16 {
		t.Errorf("expected 16-char hex hash basename, got %q (len=%d)", base, len(base))
	}
}

func TestEnsureCacheDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	err := EnsureCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cacheDir := filepath.Join(tmpHome, ".gridctl", "cache")
	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatalf("cache dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected cache path to be a directory")
	}
}

func TestEnsureReposCacheDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	err := EnsureReposCacheDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reposDir := filepath.Join(tmpHome, ".gridctl", "cache", "repos")
	info, err := os.Stat(reposDir)
	if err != nil {
		t.Fatalf("repos cache dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected repos cache path to be a directory")
	}
}

func TestEnsureCacheDir_Idempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := EnsureCacheDir(); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := EnsureCacheDir(); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestCleanCache(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	if err := EnsureCacheDir(); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Create a file in cache to verify it gets removed
	cacheDir := filepath.Join(tmpHome, ".gridctl", "cache")
	testFile := filepath.Join(cacheDir, "testfile")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := CleanCache()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Error("expected cache dir to be removed")
	}
}

func TestCleanCache_NonExistent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	err := CleanCache()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
