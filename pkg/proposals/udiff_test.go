package proposals

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
)

// splitLines keeps each line's newline and adds no phantom trailing line,
// unlike difflib.SplitLines, so fixtures describe the real file.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// genDiff generates a unified diff section with go-difflib. An empty
// before with added set produces a /dev/null section.
func genDiff(t *testing.T, path, before, after string, added bool) string {
	t.Helper()
	from, to := "a/"+path, "b/"+path
	if added {
		from = "/dev/null"
	}
	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        splitLines(before),
		B:        splitLines(after),
		FromFile: from,
		ToFile:   to,
		Context:  3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if text == "" {
		t.Fatalf("no diff generated for %s", path)
	}
	return text
}

func applyText(t *testing.T, diff, current string, exists bool) (string, error) {
	t.Helper()
	patches, err := ParseUnified(diff)
	if err != nil {
		return "", err
	}
	if len(patches) != 1 {
		t.Fatalf("patches = %d", len(patches))
	}
	var in []byte
	if exists {
		in = []byte(current)
	}
	out, err := ApplyStrict(in, patches[0])
	return string(out), err
}

func TestParseAndApply_Modify(t *testing.T) {
	before := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	after := "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\neleven\n"
	diff := genDiff(t, "notes.md", before, after, false)
	patches, err := ParseUnified(diff)
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) != 1 || patches[0].Path != "notes.md" || patches[0].New || len(patches[0].Hunks) != 2 {
		t.Fatalf("patches = %+v", patches)
	}
	got, err := applyText(t, diff, before, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != after {
		t.Fatalf("applied = %q", got)
	}
}

func TestParseAndApply_AddedFile(t *testing.T) {
	after := "# Runbook\n\nstep one\n"
	diff := genDiff(t, "references/runbook.md", "", after, true)
	patches, err := ParseUnified(diff)
	if err != nil {
		t.Fatal(err)
	}
	if !patches[0].New || patches[0].Path != "references/runbook.md" {
		t.Fatalf("patch = %+v", patches[0])
	}
	got, err := applyText(t, diff, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != after {
		t.Fatalf("applied = %q", got)
	}
	if _, err := applyText(t, diff, "exists\n", true); !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("added over existing err = %v", err)
	}
}

func TestApply_NoNewlineAtEOF(t *testing.T) {
	t.Run("old side lacks newline, new side adds one", func(t *testing.T) {
		diff := "--- a/f.md\n+++ b/f.md\n@@ -1,2 +1,2 @@\n one\n-two\n\\ No newline at end of file\n+two\n"
		got, err := applyText(t, diff, "one\ntwo", true)
		if err != nil {
			t.Fatal(err)
		}
		if got != "one\ntwo\n" {
			t.Fatalf("applied = %q", got)
		}
	})
	t.Run("preserved on both sides", func(t *testing.T) {
		diff := "--- a/f.md\n+++ b/f.md\n@@ -1,2 +1,2 @@\n-one\n+ONE\n two\n\\ No newline at end of file\n"
		got, err := applyText(t, diff, "one\ntwo", true)
		if err != nil {
			t.Fatal(err)
		}
		if got != "ONE\ntwo" {
			t.Fatalf("applied = %q", got)
		}
	})
	t.Run("new side drops newline", func(t *testing.T) {
		diff := "--- a/f.md\n+++ b/f.md\n@@ -1 +1 @@\n-one\n+one\n\\ No newline at end of file\n"
		got, err := applyText(t, diff, "one\n", true)
		if err != nil {
			t.Fatal(err)
		}
		if got != "one" {
			t.Fatalf("applied = %q", got)
		}
	})
	t.Run("marker must match the file", func(t *testing.T) {
		diff := "--- a/f.md\n+++ b/f.md\n@@ -1 +1 @@\n-one\n\\ No newline at end of file\n+uno\n"
		if _, err := applyText(t, diff, "one\n", true); !errors.Is(err, ErrApplyFailed) {
			t.Fatalf("err = %v", err)
		}
		diff = "--- a/f.md\n+++ b/f.md\n@@ -1 +1 @@\n-one\n+uno\n"
		if _, err := applyText(t, diff, "one", true); !errors.Is(err, ErrApplyFailed) {
			t.Fatalf("missing marker err = %v", err)
		}
	})
}

func TestApply_CRLFInput(t *testing.T) {
	current := "alpha\r\nbeta\r\ngamma\r\n"
	diff := strings.ReplaceAll(genDiff(t, "f.md", "alpha\nbeta\ngamma\n", "alpha\nBETA\ngamma\n", false), "\n", "\r\n")
	got, err := applyText(t, diff, current, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != "alpha\nBETA\ngamma\n" {
		t.Fatalf("applied = %q", got)
	}
}

func TestApply_MismatchedHunk(t *testing.T) {
	diff := genDiff(t, "SKILL.md", "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n", "a\nB\nc\nd\ne\nf\ng\nh\ni\nj\nK\nl\n", false)
	current := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nsecret-content\nl\n"
	_, err := applyText(t, diff, current, true)
	if !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "hunk 2 of SKILL.md does not match the current content") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret-content") {
		t.Fatalf("error quotes content: %v", err)
	}
	// No offset search: the same change one line lower does not apply.
	shifted := "x\n" + "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	if _, err := applyText(t, diff, shifted, true); !errors.Is(err, ErrApplyFailed) {
		t.Fatalf("fuzzy apply err = %v", err)
	}
}

func TestParseUnified_Refusals(t *testing.T) {
	mod := func(path string) string {
		return "--- a/" + path + "\n+++ b/" + path + "\n@@ -1 +1 @@\n-x\n+y\n"
	}
	cases := []struct {
		name string
		diff string
		want error
	}{
		{"empty", "", ErrInvalidInput},
		{"prose only", "please apply this change\n", ErrInvalidInput},
		{"no hunks", "--- a/f.md\n+++ b/f.md\n", ErrInvalidInput},
		{"short hunk", "--- a/f.md\n+++ b/f.md\n@@ -1,2 +1,2 @@\n-x\n+y\n", ErrInvalidInput},
		{"long hunk", "--- a/f.md\n+++ b/f.md\n@@ -1 +1 @@\n-x\n-z\n+y\n", ErrInvalidInput},
		{"bad header", "--- a/f.md\n+++ b/f.md\n@@ -a +1 @@\n-x\n+y\n", ErrInvalidInput},
		{"duplicate section", mod("f.md") + mod("f.md"), ErrInvalidInput},
		{"deletion", "--- a/f.md\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n", ErrDiffUnsupported},
		{"rename", "--- a/f.md\n+++ b/g.md\n@@ -1 +1 @@\n-x\n+y\n", ErrDiffUnsupported},
		{"git rename header", "diff --git a/f.md b/g.md\nsimilarity index 90%\nrename from f.md\nrename to g.md\n", ErrDiffUnsupported},
		{"binary", "diff --git a/f.png b/f.png\nBinary files a/f.png and b/f.png differ\n", ErrDiffUnsupported},
		{"mode change", "diff --git a/f.sh b/f.sh\nold mode 100644\nnew mode 100755\n", ErrDiffUnsupported},
		{"deleted file mode", "diff --git a/f.md b/f.md\ndeleted file mode 100644\n", ErrDiffUnsupported},
		{"absolute", mod("/etc/passwd"), ErrDiffUnsupported},
		{"escape", mod("../other/SKILL.md"), ErrDiffUnsupported},
		{"inner escape", mod("refs/../../x.md"), ErrDiffUnsupported},
		{"dotfile", mod(".origin.json"), ErrDiffUnsupported},
		{"dot directory", mod("refs/.git/config"), ErrDiffUnsupported},
		{"tmp", mod("SKILL.md.tmp"), ErrDiffUnsupported},
		{"backslash", mod(`refs\x.md`), ErrDiffUnsupported},
		{"added with offset", "--- /dev/null\n+++ b/n.md\n@@ -1,0 +1 @@\n+x\n", ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseUnified(tc.diff)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestParseUnified_PreambleAndMultipleFiles(t *testing.T) {
	diff := "diff --git a/SKILL.md b/SKILL.md\nindex 111..222 100644\n" +
		genDiff(t, "SKILL.md", "a\n", "b\n", false) +
		"diff --git a/refs/new.md b/refs/new.md\nnew file mode 100644\n" +
		genDiff(t, "refs/new.md", "", "n\n", true)
	patches, err := ParseUnified(diff)
	if err != nil {
		t.Fatal(err)
	}
	got := patchPaths(patches)
	if strings.Join(got, ",") != "SKILL.md,refs/new.md" {
		t.Fatalf("paths = %v", got)
	}
	// A removed line that itself starts with "-- " is not a new header.
	tricky := "--- a/f.md\n+++ b/f.md\n@@ -1,2 +1,1 @@\n--- not a header\n keep\n"
	out, err := applyText(t, tricky, "-- not a header\nkeep\n", true)
	if err != nil {
		t.Fatal(err)
	}
	if out != "keep\n" {
		t.Fatalf("applied = %q", out)
	}
}

// pathFixtures is the shared list the diff path validator and
// skillpins.DigestedFile must agree on. nested marks paths the
// nested-skill exclusion removes from the digest set.
var pathFixtures = []struct {
	path   string
	nested bool
}{
	{"SKILL.md", false},
	{"notes.md", false},
	{"references/runbook.md", false},
	{"scripts/check.sh", false},
	{"SKILL.md.bak", false},
	{".origin.json", false},
	{".hidden/notes.md", false},
	{"references/.draft.md", false},
	{"notes.md.tmp", false},
	{"references/runbook.tmp", false},
	{"child/SKILL.md", true},
	{"child/notes.md", true},
	{"deep/er/SKILL.md", true},
}

func TestPathValidator_AgreesWithDigestedFile(t *testing.T) {
	roots := []string{"child/"}
	for _, fx := range pathFixtures {
		t.Run(fx.path, func(t *testing.T) {
			clean, err := cleanPatchPath(fx.path)
			if err == nil {
				err = validateSkillPath(clean, roots)
			}
			accepted := err == nil
			hashed := !fx.nested && (fx.path == "SKILL.md" || skillpins.DigestedFile(registry.SkillFile{Path: filepath.FromSlash(fx.path)}))
			if accepted != hashed {
				t.Fatalf("validator accepted=%v (err %v), digest set includes=%v", accepted, err, hashed)
			}
			if !accepted && !errors.Is(err, ErrDiffUnsupported) {
				t.Fatalf("refusal is not ErrDiffUnsupported: %v", err)
			}
		})
	}
}

// TestPathValidator_StricterThanRegistry checks that every accepted path is
// also accepted by the registry's own traversal check.
func TestPathValidator_StricterThanRegistry(t *testing.T) {
	reg := registry.NewStore(t.TempDir())
	if err := reg.SaveSkill(&registry.AgentSkill{Name: "probe", Description: "probe skill", Body: "body\n"}); err != nil {
		t.Fatal(err)
	}
	for _, fx := range pathFixtures {
		clean, err := cleanPatchPath(fx.path)
		if err != nil || validateSkillPath(clean, []string{"child/"}) != nil || clean == "SKILL.md" {
			continue
		}
		if err := reg.WriteFile("probe", clean, []byte("x\n")); err != nil {
			t.Fatalf("registry refused accepted path %s: %v", clean, err)
		}
	}
	for _, bad := range []string{"../escape.md", "/abs.md"} {
		if _, err := cleanPatchPath(bad); err == nil {
			t.Fatalf("validator accepted %s", bad)
		}
	}
}
