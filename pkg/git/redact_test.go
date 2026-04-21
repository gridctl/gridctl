package git

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	cases := []struct {
		in, out string
	}{
		{"", ""},
		{"https://example.com/foo/bar", "https://example.com/foo/bar"},
		{"https://TOKEN@github.com/org/repo", "https://github.com/org/repo"},
		{"https://user:password@git.example.com/org/repo.git", "https://git.example.com/org/repo.git"},
		{"http://ghp_abc@host/path", "http://host/path"},
		{"git@github.com:org/repo.git", "git@github.com:org/repo.git"}, // SCP-style unchanged
		{"ssh://git@github.com/org/repo", "ssh://github.com/org/repo"},
		{"just some text", "just some text"},
		{"/local/path/to/repo", "/local/path/to/repo"},
	}
	for _, c := range cases {
		if got := RedactURL(c.in); got != c.out {
			t.Errorf("RedactURL(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestRedactString_GitHubClassicPAT(t *testing.T) {
	in := "push failed with ghp_" + strings.Repeat("a", 36)
	got := RedactString(in)
	if strings.Contains(got, "ghp_") {
		t.Errorf("expected token stripped, got %q", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected redaction marker, got %q", got)
	}
}

func TestRedactString_GitHubFineGrainedPAT(t *testing.T) {
	in := "clone failed: github_pat_" + strings.Repeat("x", 82)
	got := RedactString(in)
	if strings.Contains(got, "github_pat_") {
		t.Errorf("expected fine-grained PAT stripped, got %q", got)
	}
}

func TestRedactString_GitLabPAT(t *testing.T) {
	in := "auth error: glpat-" + strings.Repeat("b", 20)
	got := RedactString(in)
	if strings.Contains(got, "glpat-") {
		t.Errorf("expected GitLab PAT stripped, got %q", got)
	}
}

func TestRedactString_EmbeddedUserinfo(t *testing.T) {
	in := "fetch failed for https://abcdef@example.com/repo"
	got := RedactString(in)
	if strings.Contains(got, "abcdef@") {
		t.Errorf("expected userinfo stripped, got %q", got)
	}
	if !strings.Contains(got, "https://example.com/repo") {
		t.Errorf("expected cleaned URL, got %q", got)
	}
}

func TestRedactString_NoChange(t *testing.T) {
	in := "all clean, nothing to redact here"
	if got := RedactString(in); got != in {
		t.Errorf("expected unchanged, got %q", got)
	}
}

func TestRedactError_Nil(t *testing.T) {
	if RedactError(nil) != nil {
		t.Error("RedactError(nil) should be nil")
	}
}

func TestRedactError_Unchanged(t *testing.T) {
	err := errors.New("clean error message")
	if got := RedactError(err); got != err {
		t.Errorf("expected identical error returned, got %v", got)
	}
}

func TestRedactError_Wraps(t *testing.T) {
	base := errors.New("base cause")
	wrapped := fmt.Errorf("fetch https://ghp_%s@host failed: %w",
		strings.Repeat("x", 36), base)
	red := RedactError(wrapped)
	if strings.Contains(red.Error(), "ghp_") {
		t.Errorf("redacted message still contains token: %q", red.Error())
	}
	if !errors.Is(red, base) {
		t.Error("expected errors.Is to see through redaction wrapper")
	}
}
