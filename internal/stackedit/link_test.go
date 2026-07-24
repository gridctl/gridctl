package stackedit

import (
	"errors"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

const linkSource = `# stack heading comment
version: "1"
name: demo
mcp-servers:
  - name: github # inline comment
    command: [npx, github-mcp]
link:
  - claude
  - client: cursor
    group: dev
`

func TestUpsertLinkEntry_AppendsShorthand(t *testing.T) {
	out, err := UpsertLinkEntry([]byte(linkSource), config.LinkEntry{Client: "grok"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "# stack heading comment") || !strings.Contains(text, "# inline comment") {
		t.Errorf("comments not preserved:\n%s", text)
	}
	if !strings.Contains(text, "- grok") {
		t.Errorf("shorthand entry missing:\n%s", text)
	}
	if strings.Contains(text, "client: grok") {
		t.Errorf("slug-only entry must stay scalar shorthand:\n%s", text)
	}
	// Existing entries keep their forms and order.
	if strings.Index(text, "- claude") > strings.Index(text, "client: cursor") {
		t.Errorf("entry order changed:\n%s", text)
	}
}

func TestUpsertLinkEntry_ReplacesExistingByClient(t *testing.T) {
	out, err := UpsertLinkEntry([]byte(linkSource), config.LinkEntry{Client: "claude", Group: "release"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "client: claude") || !strings.Contains(text, "group: release") {
		t.Errorf("shorthand not promoted with new options:\n%s", text)
	}
	if strings.Count(text, "claude") != 1 {
		t.Errorf("expected exactly one claude entry:\n%s", text)
	}
}

func TestUpsertLinkEntry_UpdatesMappingEntry(t *testing.T) {
	out, err := UpsertLinkEntry([]byte(linkSource), config.LinkEntry{Client: "cursor"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "- cursor") || strings.Contains(text, "group: dev") {
		t.Errorf("option-less upsert should collapse to shorthand and drop group:\n%s", text)
	}
}

func TestUpsertLinkEntry_CreatesMissingBlock(t *testing.T) {
	src := "version: \"1\"\nname: demo\n"
	out, err := UpsertLinkEntry([]byte(src), config.LinkEntry{Client: "claude"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "link:") || !strings.Contains(text, "- claude") {
		t.Errorf("block not created:\n%s", text)
	}
	if strings.Contains(text, "link: [") {
		t.Errorf("flow style leaked:\n%s", text)
	}
}

func TestUpsertLinkEntry_RequiresClient(t *testing.T) {
	if _, err := UpsertLinkEntry([]byte(linkSource), config.LinkEntry{}); err == nil {
		t.Fatal("want error for empty client")
	}
}

func TestRemoveLinkEntry_ScalarAndMapping(t *testing.T) {
	out, err := RemoveLinkEntry([]byte(linkSource), "claude")
	if err != nil {
		t.Fatalf("remove scalar: %v", err)
	}
	if strings.Contains(string(out), "- claude") {
		t.Errorf("scalar entry not removed:\n%s", out)
	}

	out2, err := RemoveLinkEntry(out, "cursor")
	if err != nil {
		t.Fatalf("remove mapping: %v", err)
	}
	text := string(out2)
	if strings.Contains(text, "cursor") {
		t.Errorf("mapping entry not removed:\n%s", text)
	}
	if strings.Contains(text, "link:") {
		t.Errorf("empty link block should drop the key:\n%s", text)
	}
	if !strings.Contains(text, "# stack heading comment") {
		t.Errorf("comments lost:\n%s", text)
	}
}

func TestRemoveLinkEntry_Errors(t *testing.T) {
	if _, err := RemoveLinkEntry([]byte("name: demo\n"), "claude"); !errors.Is(err, ErrNoLinkBlock) {
		t.Fatalf("want ErrNoLinkBlock, got %v", err)
	}
	if _, err := RemoveLinkEntry([]byte(linkSource), "grok"); !errors.Is(err, ErrEntryNotDeclared) {
		t.Fatalf("want ErrEntryNotDeclared, got %v", err)
	}
	// A parse failure must be distinguishable from "not declared".
	if _, err := RemoveLinkEntry([]byte(":\n  broken: [\n"), "claude"); err == nil ||
		errors.Is(err, ErrNoLinkBlock) || errors.Is(err, ErrEntryNotDeclared) {
		t.Fatalf("parse failure must not map to a declared-state sentinel, got %v", err)
	}
}

func TestUpsertLinkEntry_RejectsNonSequenceValue(t *testing.T) {
	src := "name: demo\nlink: not-a-sequence\n"
	if _, err := UpsertLinkEntry([]byte(src), config.LinkEntry{Client: "claude"}); err == nil ||
		!strings.Contains(err.Error(), "not a sequence") {
		t.Fatalf("want not-a-sequence error, got %v", err)
	}
}

func TestLinkEntry_AliasNodes(t *testing.T) {
	src := `name: demo
defaults:
  - &cl claude
link:
  - *cl
  - cursor
`
	// Upsert must match the alias, not append a duplicate declaration.
	out, err := UpsertLinkEntry([]byte(src), config.LinkEntry{Client: "claude", Group: "dev"})
	if err != nil {
		t.Fatalf("upsert over alias: %v", err)
	}
	if strings.Count(string(out), "claude") < 1 && strings.Count(string(out), "*cl") < 1 {
		t.Fatalf("claude entry lost:\n%s", out)
	}
	// The anchored source under defaults: stays; the link entry is replaced.
	if !strings.Contains(string(out), "group: dev") {
		t.Errorf("alias entry not replaced with options:\n%s", out)
	}

	// Remove must also resolve the alias.
	out2, err := RemoveLinkEntry([]byte(src), "claude")
	if err != nil {
		t.Fatalf("remove over alias: %v", err)
	}
	if strings.Contains(string(out2), "*cl") {
		t.Errorf("alias entry not removed:\n%s", out2)
	}
}
