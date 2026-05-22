//go:build integration

package integration

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

// TestAnthropicSkillCompat asserts that a real prompt-only skill from
// github.com/anthropics/skills loads through gridctl's registry walker
// unchanged: store.Load() succeeds, the skill validates, and the body
// surfaces verbatim. The fixture lives at testdata/anthropic_skills/ so a
// future spec change shows up as a fixture refresh, not a test rewrite.
//
// This is the gridctl side of the agentskills.io compatibility contract:
// every published Anthropic skill should drop into a gridctl registry
// without a transform step.
func TestAnthropicSkillCompat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	const fixtureSkill = "brand-guidelines"
	fixtureDir := filepath.Join("testdata", "anthropic_skills", fixtureSkill)
	fixturePath := filepath.Join(fixtureDir, "SKILL.md")

	fixtureBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture %s: %v", fixturePath, err)
	}
	if len(fixtureBytes) == 0 {
		t.Fatalf("fixture %s is empty", fixturePath)
	}

	// Stand up a registry root with the canonical skills/<name>/SKILL.md
	// layout so the walker sees it the way it would see a user's skill.
	tmpRoot := t.TempDir()
	skillDir := filepath.Join(tmpRoot, "skills", fixtureSkill)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	dstPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(dstPath, fixtureBytes, 0o644); err != nil {
		t.Fatalf("write SKILL.md to temp registry: %v", err)
	}

	// 1. store.Load() must succeed against the verbatim Anthropic SKILL.md.
	store := registry.NewStore(tmpRoot)
	if err := store.Load(); err != nil {
		t.Fatalf("store.Load(): %v", err)
	}

	// 2. The skill must be discoverable by name.
	sk, err := store.GetSkill(fixtureSkill)
	if err != nil {
		t.Fatalf("GetSkill(%q): %v", fixtureSkill, err)
	}

	// 3. Frontmatter fields the spec requires must be populated.
	if sk.Name != fixtureSkill {
		t.Errorf("Name = %q, want %q", sk.Name, fixtureSkill)
	}
	if sk.Description == "" {
		t.Error("Description is empty — required by the agentskills.io spec")
	}

	// 4. Body must surface unchanged. The walker parses frontmatter off
	//    the top of the file; everything after the closing --- is the body.
	wantBody := bodyAfterFrontmatter(t, fixtureBytes)
	if sk.Body != wantBody {
		t.Errorf("body did not surface verbatim\n got (%d bytes): %q\nwant (%d bytes): %q",
			len(sk.Body), truncForLog(sk.Body),
			len(wantBody), truncForLog(wantBody),
		)
	}

	// 5. ValidateSkillFull must report no errors. This is what
	//    `gridctl skill validate brand-guidelines` runs under the hood,
	//    so a passing assertion here is "the CLI would print ✓ valid".
	result := registry.ValidateSkillFull(sk)
	if !result.Valid() {
		t.Errorf("ValidateSkillFull errors: %v", result.Errors)
	}
}

// bodyAfterFrontmatter returns the bytes after the closing `---` line of
// the SKILL.md frontmatter block. Tests assert against this directly so a
// body regression in the walker shows up as a byte-level mismatch, not a
// downstream test failure several layers deep. The implementation matches
// pkg/registry/frontmatter.go ParseSkillMD's body extraction: skip past
// the closing `---` line (delimiter + trailing newline) and trim a single
// leading newline so the assertion compares apples-to-apples.
func bodyAfterFrontmatter(t *testing.T, raw []byte) string {
	t.Helper()
	const delim = "---"
	first := bytes.Index(raw, []byte(delim))
	if first < 0 {
		t.Fatalf("fixture has no opening frontmatter delimiter")
	}
	rest := raw[first+len(delim):]
	// "\n---\n" matches the closing delimiter line specifically — `---` on
	// its own line with a trailing newline. This is what the walker keys
	// off; matching the same shape avoids false hits on `---` inside the
	// body (markdown horizontal rules).
	close := bytes.Index(rest, []byte("\n"+delim+"\n"))
	if close < 0 {
		t.Fatalf("fixture has no closing frontmatter delimiter")
	}
	body := rest[close+len("\n"+delim+"\n"):]
	return strings.TrimPrefix(string(body), "\n")
}

// truncForLog clips long bodies in test failure output so the line stays
// readable. The full body is on disk if a real diff is needed.
func truncForLog(s string) string {
	const max = 120
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
