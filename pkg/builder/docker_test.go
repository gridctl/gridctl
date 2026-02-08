package builder

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStreamBuildOutput_Success(t *testing.T) {
	output := `{"stream":"Step 1/3 : FROM alpine:latest\n"}
{"stream":"Step 2/3 : RUN echo hello\n"}
{"stream":"Step 3/3 : CMD [\"echo\"]\n"}
{"aux":{"ID":"sha256:abc123def456"}}
{"stream":"Successfully built abc123def456\n"}`

	reader := strings.NewReader(output)
	logger := newTestLogger()

	imageID, err := streamBuildOutput(reader, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imageID != "sha256:abc123def456" {
		t.Errorf("expected image ID 'sha256:abc123def456', got %q", imageID)
	}
}

func TestStreamBuildOutput_Error(t *testing.T) {
	output := `{"stream":"Step 1/2 : FROM alpine:latest\n"}
{"error":"failed to build","errorDetail":{"message":"command failed"}}`

	reader := strings.NewReader(output)
	logger := newTestLogger()

	_, err := streamBuildOutput(reader, logger)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to build") {
		t.Errorf("expected error containing 'failed to build', got %q", err.Error())
	}
}

func TestStreamBuildOutput_EmptyStream(t *testing.T) {
	reader := strings.NewReader("")
	logger := newTestLogger()

	imageID, err := streamBuildOutput(reader, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imageID != "" {
		t.Errorf("expected empty image ID, got %q", imageID)
	}
}

func TestStreamBuildOutput_NoAux(t *testing.T) {
	output := `{"stream":"Step 1/1 : FROM alpine:latest\n"}
{"stream":"Successfully built abc123\n"}`

	reader := strings.NewReader(output)
	logger := newTestLogger()

	imageID, err := streamBuildOutput(reader, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imageID != "" {
		t.Errorf("expected empty image ID without aux, got %q", imageID)
	}
}

func TestStreamBuildOutput_InvalidJSON(t *testing.T) {
	output := `not valid json`

	reader := strings.NewReader(output)
	logger := newTestLogger()

	_, err := streamBuildOutput(reader, logger)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGetExcludePatterns_Default(t *testing.T) {
	dir := t.TempDir()

	patterns := getExcludePatterns(dir)
	if len(patterns) == 0 {
		t.Fatal("expected default patterns")
	}

	expected := []string{".git", ".gitignore", "node_modules", "__pycache__", "*.pyc", ".env", ".env.*"}
	for _, exp := range expected {
		found := false
		for _, p := range patterns {
			if p == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected pattern %q in defaults", exp)
		}
	}
}

func TestGetExcludePatterns_WithDockerignore(t *testing.T) {
	dir := t.TempDir()

	content := "build/\n# comment\n\ntmp/\n*.log\n"
	if err := os.WriteFile(filepath.Join(dir, ".dockerignore"), []byte(content), 0644); err != nil {
		t.Fatalf("write .dockerignore: %v", err)
	}

	patterns := getExcludePatterns(dir)

	customPatterns := []string{"build/", "tmp/", "*.log"}
	for _, exp := range customPatterns {
		found := false
		for _, p := range patterns {
			if p == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected pattern %q from .dockerignore", exp)
		}
	}

	for _, p := range patterns {
		if strings.HasPrefix(p, "#") {
			t.Errorf("comment should not be in patterns: %q", p)
		}
	}
}

func TestCreateTarFromDir(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("world"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	reader, err := CreateTarFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer reader.Close()

	// Read tar entries to verify contents
	tr := tar.NewReader(reader)
	entries := make(map[string]bool)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar entry: %v", err)
		}
		entries[hdr.Name] = true
	}

	if !entries["file1.txt"] {
		t.Error("expected file1.txt in tar archive")
	}
	if !entries[filepath.Join("subdir", "file2.txt")] {
		t.Error("expected subdir/file2.txt in tar archive")
	}
}

func TestCreateTarFromDir_SkipsGit(t *testing.T) {
	dir := t.TempDir()

	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	reader, err := CreateTarFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer reader.Close()

	// Verify tar contents: main.go should be present, .git/* should be absent
	tr := tar.NewReader(reader)
	foundMain := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading tar entry: %v", err)
		}
		if strings.HasPrefix(hdr.Name, ".git") {
			t.Errorf("expected .git to be excluded, found %q", hdr.Name)
		}
		if hdr.Name == "main.go" {
			foundMain = true
		}
	}
	if !foundMain {
		t.Error("expected main.go in tar archive")
	}
}
