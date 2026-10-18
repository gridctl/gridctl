package vault

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadPassphraseFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pass")
	if err := os.WriteFile(path, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadPassphraseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" {
		t.Fatalf("got %q", got)
	}
}

func TestReadPassphraseFile_RejectsModeOwnerAndSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pass")
	if err := os.WriteFile(path, []byte("secret-value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadPassphraseFile(path)
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("mode error = %v", err)
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Fatalf("error = %v", err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	prev := passphraseFileOwner
	passphraseFileOwner = func(os.FileInfo) (uint32, bool) { return currentUID() + 1, true }
	t.Cleanup(func() { passphraseFileOwner = prev })
	_, err = ReadPassphraseFile(path)
	if err == nil || strings.Contains(err.Error(), "secret-value") || !strings.Contains(err.Error(), "owner") {
		t.Fatalf("owner error = %v", err)
	}

	passphraseFileOwner = prev
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", maxPassphraseFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = ReadPassphraseFile(big)
	if err == nil || strings.Contains(err.Error(), "xxx") || !strings.Contains(err.Error(), "4 KiB") {
		t.Fatalf("size error = %v", err)
	}

	if err := os.Mkdir(filepath.Join(dir, "dirpass"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = ReadPassphraseFile(filepath.Join(dir, "dirpass"))
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory error = %v", err)
	}
}
