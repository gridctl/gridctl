package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/internal/importer"
	"github.com/gridctl/gridctl/pkg/contexts"
)

func TestUnifiedImport_FlagContract(t *testing.T) {
	bin := buildImportBinary(t)
	home := t.TempDir()
	cwd := t.TempDir()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no client", []string{"import", "--kind", "all"}, "requires a client argument"},
		{"unknown kind", []string{"import", "opencode", "--kind", "bogus"}, `unknown kind "bogus" (supported: servers, skills, agents, context, all)`},
		{"fragment without context", []string{"import", "opencode", "--kind", "skills", "--context-fragment", "x"}, "--context-fragment requires --kind context"},
		{"trust without skills", []string{"import", "opencode", "--kind", "context", "--trust"}, "--trust requires --kind skills or agents"},
		{"no-activate without skills", []string{"import", "opencode", "--kind", "context", "--no-activate"}, "--no-activate requires --kind skills or agents"},
		{"bad fragment name", []string{"import", "opencode", "--kind", "context", "--context-fragment", "Bad Name"}, "fragment name must be lowercase letters, digits, and hyphens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runImportBin(t, bin, home, cwd, tc.args...)
			if code != 1 || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d stderr=%s", code, stderr)
			}
		})
	}

	t.Run("unknown client lists the union", func(t *testing.T) {
		_, stderr, code := runImportBin(t, bin, home, cwd, "import", "nosuch", "--kind", "all")
		if code != 1 || !strings.Contains(stderr, `unknown client "nosuch"`) || !strings.Contains(stderr, "Supported clients:") {
			t.Fatalf("exit %d stderr=%s", code, stderr)
		}
		for _, slug := range []string{"agents", "cursor", "gemini"} {
			if !strings.Contains(stderr, slug) {
				t.Fatalf("supported list missing %s: %s", slug, stderr)
			}
		}
	})

	t.Run("kind servers keeps the provisioner error", func(t *testing.T) {
		_, servers, code := runImportBin(t, bin, home, cwd, "import", "nosuch", "--kind", "servers")
		_, def, defCode := runImportBin(t, bin, home, cwd, "import", "nosuch")
		if code != 1 || defCode != 1 || servers != def {
			t.Fatalf("servers-only error drifted\nkind=%s\ndefault=%s", servers, def)
		}
	})
}

func TestUnifiedImport_OpenCodeAll(t *testing.T) {
	bin := buildImportBinary(t)
	home := t.TempDir()
	root := t.TempDir()
	writeOpenCodeImportHome(t, home, "# Personal prefs\n\nBe terse.\n")
	stack := writeStack(t, root)
	beforeStack := hashFile(t, stack)
	ctxDir := filepath.Join(home, ".gridctl", "context")

	stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--file", stack)
	if code != 0 {
		t.Fatalf("exit %d\nstdout=%s\nstderr=%s", code, stdout, stderr)
	}
	if !strings.Contains(string(mustRead(t, stack)), "oc-bin") {
		t.Fatalf("stack missing server:\n%s", mustRead(t, stack))
	}
	if matches, _ := filepath.Glob(stack + ".gridctl-backup-*"); len(matches) != 1 {
		t.Fatalf("backups = %v", matches)
	}
	for _, name := range []string{"oc-one", "oc-two"} {
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "registry", "skills", name, "SKILL.md")); err != nil {
			t.Fatalf("skill %s: %v", name, err)
		}
	}
	if !strings.Contains(stdout, "OpenCode agent dialect is not imported in this release") {
		t.Fatalf("agent row missing dialect skip:\n%s", stdout)
	}
	canon := filepath.Join(ctxDir, "AGENTS.md")
	if got := string(mustRead(t, canon)); !strings.Contains(got, "Personal prefs") {
		t.Fatalf("canonical = %q", got)
	}
	for _, want := range []string{"KIND", "NAME", "SOURCE", "ACTION", "servers", "skills", "agents", "context", "gridctl apply " + stack, "gridctl ctx sync --dry-run"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q\n%s", want, stdout)
		}
	}
	if hashFile(t, stack) == beforeStack {
		t.Fatal("apply did not change the stack")
	}

	t.Run("dry-run writes nothing", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeOpenCodeImportHome(t, home, "# Personal prefs\n")
		stack := writeStack(t, root)
		before := hashTree(t, home, stack)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--dry-run", "--file", stack)
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		if !strings.Contains(stdout, "would import") {
			t.Fatalf("missing would import:\n%s", stdout)
		}
		if hashTree(t, home, stack) != before {
			t.Fatal("dry-run changed files")
		}
	})
}

func TestUnifiedImport_ContextRows(t *testing.T) {
	bin := buildImportBinary(t)
	begin := "<!-- BEGIN GRIDCTL MANAGED -->"
	header := "<!-- Managed by gridctl. Edit with 'gridctl ctx edit' or the web UI; this content is overwritten on sync. Content outside the gridctl block is yours. -->"

	t.Run("shim only skips", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		mgr := contexts.NewManagerWithHome(home)
		writeOpenCodeImportHome(t, home, "@"+mgr.CanonicalPath()+"\n")
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--dry-run")
		if code != 1 || !strings.Contains(stdout, "would skip: empty after removing gridctl-managed content") {
			t.Fatalf("dry exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context")
		if code != 1 || !strings.Contains(stdout, "skipped: empty after removing gridctl-managed content") {
			t.Fatalf("apply exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
	})

	t.Run("block body is adopted", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		body := begin + "\n" + header + "\n\nblock body\n<!-- END GRIDCTL MANAGED -->\n"
		writeOpenCodeImportHome(t, home, body)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--dry-run", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("dry exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		dry := parseUnified(t, stdout)
		if dry.Kinds.Context.Action != "would import" {
			t.Fatalf("dry action = %s", dry.Kinds.Context.Action)
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("apply exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		apply := parseUnified(t, stdout)
		if apply.Kinds.Context.Action != "imported" || apply.Kinds.Context.Bytes != dry.Kinds.Context.Bytes || apply.Kinds.Context.Bytes == 0 {
			t.Fatalf("apply context = %+v dry bytes %d", apply.Kinds.Context, dry.Kinds.Context.Bytes)
		}
		got := string(mustRead(t, filepath.Join(home, ".gridctl", "context", "AGENTS.md")))
		if got != "block body\n" {
			t.Fatalf("canonical = %q", got)
		}
	})

	t.Run("text around block", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		body := "before text\n\n" + begin + "\n" + header + "\n\nblock body\n<!-- END GRIDCTL MANAGED -->\n\nafter text\n"
		writeOpenCodeImportHome(t, home, body)
		_, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--yes")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s", code, stderr)
		}
		got := string(mustRead(t, filepath.Join(home, ".gridctl", "context", "AGENTS.md")))
		want := "before text\n\nafter text\n\nblock body\n"
		if got != want {
			t.Fatalf("canonical = %q", got)
		}
	})

	t.Run("malformed marker skips", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeOpenCodeImportHome(t, home, begin+"\n"+header+"\nkept body\n")
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--dry-run")
		if code != 1 || !strings.Contains(stdout, "would skip:") || !strings.Contains(stdout, begin) {
			t.Fatalf("dry exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context")
		if code != 1 || !strings.Contains(stdout, "skipped:") || !strings.Contains(stdout, begin) {
			t.Fatalf("apply exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "context", "AGENTS.md")); !os.IsNotExist(err) {
			t.Fatal("canonical file was written")
		}
	})

	t.Run("canonical exists precedes marker check", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeOpenCodeImportHome(t, home, begin+"\nkept\n")
		mgr := contexts.NewManagerWithHome(home)
		if err := mgr.SaveCanonical("already here\n"); err != nil {
			t.Fatal(err)
		}
		before := hashFile(t, mgr.CanonicalPath())
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--file", writeStack(t, root))
		if code != 0 || !strings.Contains(stdout, "skipped: canonical context exists") {
			t.Fatalf("exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "gridctl ctx init --import opencode --force") || !strings.Contains(stderr, "--context-fragment <name>") {
			t.Fatalf("follow-ups missing: %s", stderr)
		}
		if hashFile(t, mgr.CanonicalPath()) != before {
			t.Fatal("canonical changed")
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context")
		if code != 1 || !strings.Contains(stdout, "skipped: canonical context exists") {
			t.Fatalf("alone exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
	})

	t.Run("fragments mode", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeOpenCodeImportHome(t, home, "# prefs\n")
		if err := os.MkdirAll(filepath.Join(home, ".gridctl", "context", "fragments"), 0o755); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--dry-run")
		if code != 1 || !strings.Contains(stdout, "would skip: fragments mode active") || !strings.Contains(stderr, "--context-fragment <name>") {
			t.Fatalf("dry exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "context", "AGENTS.md")); !os.IsNotExist(err) {
			t.Fatal("dry-run created a canonical file")
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context")
		if code != 1 || !strings.Contains(stdout, "skipped: fragments mode active") {
			t.Fatalf("apply exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--file", writeStack(t, root))
		if code != 0 || !strings.Contains(stdout, "skipped: fragments mode active") {
			t.Fatalf("all exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
	})

	t.Run("fragment migration and exists", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeOpenCodeImportHome(t, home, "# from opencode\n")
		mgr := contexts.NewManagerWithHome(home)
		if err := mgr.SaveCanonical("# original canon\n"); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--context-fragment", "20-opencode")
		if code != 0 {
			t.Fatalf("exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "imported as fragment 20-opencode") {
			t.Fatalf("stdout = %s", stdout)
		}
		if !strings.Contains(stdout+stderr, "fragments/00-default.md") || !strings.Contains(stdout+stderr, "Backup:") {
			t.Fatalf("migration lines missing\nstdout=%s\nstderr=%s", stdout, stderr)
		}
		frag := string(mustRead(t, filepath.Join(home, ".gridctl", "context", "fragments", "20-opencode.md")))
		if !strings.Contains(frag, "from opencode") {
			t.Fatalf("fragment = %q", frag)
		}
		migrated := string(mustRead(t, filepath.Join(home, ".gridctl", "context", "fragments", "00-default.md")))
		if !strings.Contains(migrated, "original canon") {
			t.Fatalf("migrated = %q", migrated)
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--context-fragment", "20-opencode", "--dry-run")
		if code != 1 || !strings.Contains(stdout, "would skip: fragment 20-opencode exists") {
			t.Fatalf("second dry exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--context-fragment", "20-opencode")
		if code != 1 || !strings.Contains(stdout, "skipped: fragment 20-opencode exists") {
			t.Fatalf("second apply exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
	})

	t.Run("malformed fragment does not activate", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeOpenCodeImportHome(t, home, begin+"\nkept\n")
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--context-fragment", "20-opencode", "--dry-run")
		if code != 1 || !strings.Contains(stdout, begin) {
			t.Fatalf("dry exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "context", "fragments")); !os.IsNotExist(err) {
			t.Fatal("dry-run activated fragments mode")
		}
		stdout, stderr, code = runImportBin(t, bin, home, root, "import", "opencode", "--kind", "context", "--context-fragment", "20-opencode")
		if code != 1 || !strings.Contains(stdout, begin) {
			t.Fatalf("apply exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "context", "fragments")); !os.IsNotExist(err) {
			t.Fatal("apply activated fragments mode")
		}
	})
}

func TestUnifiedImport_JSONShape(t *testing.T) {
	bin := buildImportBinary(t)
	home := t.TempDir()
	root := t.TempDir()
	writeOpenCodeImportHome(t, home, "# Personal prefs\n")
	stack := writeStack(t, root)
	stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--file", stack, "--format", "json")
	if code != 0 {
		t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc unifiedImportDoc
	if err := dec.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if dec.More() {
		t.Fatal("stdout has more than one JSON value")
	}
	if doc.SchemaVersion != 1 || doc.Client != "opencode" || doc.StoppedAt != "" || len(doc.NotRun) != 0 {
		t.Fatalf("doc header = %+v", doc)
	}
	if doc.Kinds.Servers == nil || doc.Kinds.Servers.SchemaVersion != 1 || doc.Kinds.Servers.Summary.Imported < 1 {
		t.Fatalf("servers = %+v", doc.Kinds.Servers)
	}
	if doc.Kinds.Skills == nil || len(doc.Kinds.Skills.Entries) != 2 {
		t.Fatalf("skills = %+v", doc.Kinds.Skills)
	}
	if doc.Kinds.Agents == nil || len(doc.Kinds.Agents.Entries) != 1 || doc.Kinds.Agents.Entries[0].Action != "skipped" {
		t.Fatalf("agents = %+v", doc.Kinds.Agents)
	}
	if doc.Kinds.Context == nil || doc.Kinds.Context.Action != "imported" || doc.Kinds.Context.Bytes <= 0 {
		t.Fatalf("context = %+v", doc.Kinds.Context)
	}
	if !strings.Contains(stderr, "== servers ==") || !strings.Contains(stderr, "== skills ==") {
		t.Fatalf("headers missing from stderr: %s", stderr)
	}

	serverHome := t.TempDir()
	serverRoot := t.TempDir()
	writeOpenCodeImportHome(t, serverHome, "# Personal prefs\n")
	serverStack := writeStack(t, serverRoot)
	serverOut, serverErr, serverCode := runImportBin(t, bin, serverHome, serverRoot, "import", "opencode", "--yes", "--no-vault", "--file", serverStack, "--format", "json")
	if serverCode != 0 {
		t.Fatalf("standalone servers exit %d stderr=%s stdout=%s", serverCode, serverErr, serverOut)
	}
	standaloneServers := parseNormImport(t, serverOut, serverHome, serverStack)
	unifiedServers := normImportDoc(t, doc.Kinds.Servers, home, stack)
	if !reflect.DeepEqual(unifiedServers, standaloneServers) {
		t.Fatalf("kinds.servers != standalone import\nunified=%+v\nstandalone=%+v", unifiedServers, standaloneServers)
	}

	skillHome := t.TempDir()
	writeOpenCodeImportHome(t, skillHome, "# Personal prefs\n")
	skillOut, skillErr, skillCode := runImportBin(t, bin, skillHome, t.TempDir(), "skill", "import", "opencode", "--all", "--format", "json")
	if skillCode != 0 {
		t.Fatalf("standalone skills exit %d stderr=%s stdout=%s", skillCode, skillErr, skillOut)
	}
	var skillDoc skillImportDoc
	if err := json.Unmarshal([]byte(skillOut), &skillDoc); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normSkillEntries(doc.Kinds.Skills.Entries, home), normSkillEntries(filterEntries(skillDoc.Entries, "skill"), skillHome)) {
		t.Fatalf("kinds.skills entries != standalone\nunified=%+v\nstandalone=%+v", doc.Kinds.Skills.Entries, filterEntries(skillDoc.Entries, "skill"))
	}
	if !reflect.DeepEqual(normSkillEntries(doc.Kinds.Agents.Entries, home), normSkillEntries(filterEntries(skillDoc.Entries, "agent"), skillHome)) {
		t.Fatalf("kinds.agents entries != standalone\nunified=%+v\nstandalone=%+v", doc.Kinds.Agents.Entries, filterEntries(skillDoc.Entries, "agent"))
	}
}

func TestUnifiedImport_CoverageSkips(t *testing.T) {
	bin := buildImportBinary(t)

	t.Run("cursor", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeImportFixture(t, filepath.Join(home, ".cursor", "mcp.json"), `{"mcpServers":{"cur":{"command":"cursor-bin"}}}`)
		stack := writeStack(t, root)
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "cursor", "--kind", "all", "--yes", "--no-vault", "--file", stack, "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseUnified(t, stdout)
		if doc.Kinds.Servers == nil || doc.Kinds.Skills != nil || doc.Kinds.Agents != nil || doc.Kinds.Context != nil {
			t.Fatalf("kinds = %+v", doc.Kinds)
		}
		if len(doc.SkippedKinds) != 3 {
			t.Fatalf("skipped = %+v", doc.SkippedKinds)
		}
		for _, kind := range []string{"skills", "agents", "context"} {
			if !strings.Contains(stdout, "not supported for cursor") {
				t.Fatalf("missing skip for %s: %s", kind, stdout)
			}
		}
		_, stderr, code = runImportBin(t, bin, home, root, "import", "cursor", "--kind", "skills")
		if code != 1 || !strings.Contains(stderr, "no requested kind applies to cursor") || !strings.Contains(stderr, "skills: no") {
			t.Fatalf("skills-only exit %d stderr=%s", code, stderr)
		}
	})

	t.Run("agents client", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeSkillFixture(t, filepath.Join(home, ".agents", "skills", "shared-skill"), "shared-skill")
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "agents", "--kind", "all", "--yes", "--format", "json")
		if code != 0 {
			t.Fatalf("exit %d stderr=%s stdout=%s", code, stderr, stdout)
		}
		doc := parseUnified(t, stdout)
		if doc.Kinds.Servers != nil {
			t.Fatal("server scan ran for agents")
		}
		if doc.Kinds.Skills == nil || len(doc.Kinds.Skills.Entries) != 1 || doc.Kinds.Skills.Entries[0].Action != "imported" {
			t.Fatalf("skills = %+v", doc.Kinds.Skills)
		}
		if len(doc.SkippedKinds) != 3 {
			t.Fatalf("skipped = %+v", doc.SkippedKinds)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "registry", "skills", "shared-skill", "SKILL.md")); err != nil {
			t.Fatal(err)
		}
		_, stderr, code = runImportBin(t, bin, home, root, "import", "agents", "--kind", "agents")
		if code != 1 || !strings.Contains(stderr, "no requested kind applies to agents") {
			t.Fatalf("agents-only exit %d stderr=%s", code, stderr)
		}
	})

	t.Run("claude skills and agents", func(t *testing.T) {
		home := t.TempDir()
		root := t.TempDir()
		writeSkillFixture(t, filepath.Join(home, ".claude", "skills", "pcap-analysis"), "pcap-analysis")
		writeSkillFixture(t, filepath.Join(home, ".claude", "skills", "synced", "inner"), "inner")
		writeSkillFixture(t, filepath.Join(home, ".claude", "skills", "anthropic-skills", "inner"), "inner")
		writeImportFixture(t, filepath.Join(home, ".claude", "agents", "reviewer.md"), "---\nname: reviewer\ndescription: Reviews things\n---\n\nReview.\n")
		stdout, stderr, code := runImportBin(t, bin, home, root, "import", "claude-code", "--kind", "skills,agents", "--yes")
		if code != 0 {
			t.Fatalf("exit %d stdout=%s stderr=%s", code, stdout, stderr)
		}
		if strings.Count(stdout+stderr, "projecting these to claude-code") != 1 {
			t.Fatalf("projection hints not printed once\nstdout=%s\nstderr=%s", stdout, stderr)
		}
		if !strings.Contains(stdout, "claude.ai sync directory") {
			t.Fatalf("sync dirs not skipped:\n%s", stdout)
		}
		if _, err := os.Stat(filepath.Join(home, ".gridctl", "registry", "skills", "pcap-analysis", "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	})
}

func TestUnifiedImport_StopsLaterKinds(t *testing.T) {
	bin := buildImportBinary(t)
	home := t.TempDir()
	root := t.TempDir()
	writeOpenCodeImportHome(t, home, "# prefs\n")
	stack := writeStack(t, root)
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--file", stack, "--format", "json")
	if code != 2 {
		t.Fatalf("exit %d stdout=%s stderr=%s", code, stderr, stdout)
	}
	doc := parseUnified(t, stdout)
	if doc.StoppedAt != "servers" || doc.ExitCode != 2 {
		t.Fatalf("stop = %+v", doc)
	}
	if strings.Join(doc.NotRun, ",") != "skills,agents,context" {
		t.Fatalf("not_run = %v", doc.NotRun)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "context", "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatal("later kind wrote context")
	}
	if doc.Summary.Imported != 0 {
		t.Fatalf("summary = %+v", doc.Summary)
	}
	if doc.Kinds.Servers != nil && doc.Kinds.Servers.Summary.Imported != 0 {
		t.Fatalf("embedded servers summary = %+v", doc.Kinds.Servers.Summary)
	}

	textOut, textErr, textCode := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "all", "--yes", "--no-vault", "--file", stack)
	if textCode != 2 {
		t.Fatalf("text exit %d stdout=%s stderr=%s", textCode, textOut, textErr)
	}
	if strings.Contains(textOut, "imported") {
		t.Fatalf("failed write reported imported:\n%s", textOut)
	}
	if !strings.Contains(textOut, "skipped: stack write failed") {
		t.Fatalf("missing write-failed row:\n%s", textOut)
	}
}

func TestUnifiedImport_CancelledSelection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", home)
	root := t.TempDir()
	writeOpenCodeImportHome(t, home, "# prefs\n")
	stack := writeStack(t, root)
	importFile = stack
	importAll = false
	importYes = false
	importDryRun = false
	importNoVault = true
	importScopeFlag = "all"
	importSourceConfig = ""
	importProjectDir = ""
	t.Cleanup(func() {
		importFile = ""
		importAll = false
		importYes = false
		importNoVault = false
		importScopeFlag = "all"
		importSourceConfig = ""
	})
	orig := importSelector
	importSelector = func([]importer.Candidate) ([]int, error) { return nil, errPromptCancelled }
	t.Cleanup(func() { importSelector = orig })

	var stdout, stderr strings.Builder
	code := runUnifiedImport(context.Background(), &stdout, &stderr, "opencode", []string{"servers", "skills", "agents", "context"}, unifiedImportConfig{Format: "json"})
	if code != 1 {
		t.Fatalf("exit %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "Earlier kinds were written and are not rolled back.") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Error:") {
		t.Fatalf("missing Error prefix: %s", stderr.String())
	}
	doc := parseUnified(t, stdout.String())
	if doc.StoppedAt != "servers" || doc.ExitCode != 1 || strings.Join(doc.NotRun, ",") != "skills,agents,context" {
		t.Fatalf("doc = %+v", doc)
	}
	if doc.Summary.Imported != 0 {
		t.Fatalf("summary = %+v", doc.Summary)
	}

	var textOut, textErr strings.Builder
	textCode := runUnifiedImport(context.Background(), &textOut, &textErr, "opencode", []string{"servers", "skills", "agents", "context"}, unifiedImportConfig{})
	if textCode != 1 {
		t.Fatalf("text exit %d stdout=%s stderr=%s", textCode, textOut.String(), textErr.String())
	}
	if strings.Contains(textOut.String(), "imported") {
		t.Fatalf("cancelled run reported imported:\n%s", textOut.String())
	}
}

func writeOpenCodeImportHome(t *testing.T, home, agents string) {
	t.Helper()
	jsonPath, _ := openCodeCLIPaths(home)
	writeImportFixture(t, jsonPath, `{"mcp":{"ocsrv":{"type":"local","command":["oc-bin"]}}}`)
	writeSkillFixture(t, filepath.Join(home, ".config", "opencode", "skills", "oc-one"), "oc-one")
	writeSkillFixture(t, filepath.Join(home, ".config", "opencode", "skills", "oc-two"), "oc-two")
	writeImportFixture(t, filepath.Join(home, ".config", "opencode", "agents", "review.md"), "---\nmode: primary\n---\n\nReview.\n")
	writeImportFixture(t, filepath.Join(home, ".config", "opencode", "AGENTS.md"), agents)
}

func writeSkillFixture(t *testing.T, dir, name string) {
	t.Helper()
	writeImportFixture(t, filepath.Join(dir, "SKILL.md"), "---\nname: "+name+"\ndescription: Fixture skill\n---\n\nBody.\n")
}

func hashTree(t *testing.T, home, stack string) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "%x", hashFile(t, stack))
	walk := []string{
		filepath.Join(home, ".gridctl"),
		filepath.Join(home, ".config", "opencode", "AGENTS.md"),
	}
	for _, root := range walk {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			fmt.Fprintf(&b, "%s%x", path, hashFile(t, path))
			return nil
		})
	}
	return b.String()
}

type unifiedProbe struct {
	SchemaVersion int               `json:"schema_version"`
	Client        string            `json:"client"`
	StoppedAt     string            `json:"stopped_at"`
	ExitCode      int               `json:"exit_code"`
	NotRun        []string          `json:"not_run"`
	SkippedKinds  []skippedKindDoc  `json:"skipped_kinds"`
	Summary       unifiedSummaryDoc `json:"summary"`
	Kinds         struct {
		Servers *importDoc        `json:"servers"`
		Skills  *skillImportDoc   `json:"skills"`
		Agents  *skillImportDoc   `json:"agents"`
		Context *contextImportDoc `json:"context"`
	} `json:"kinds"`
}

func parseUnified(t *testing.T, stdout string) unifiedProbe {
	t.Helper()
	var doc unifiedProbe
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	return doc
}

func parseNormImport(t *testing.T, raw, home, stack string) importDoc {
	t.Helper()
	var doc importDoc
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("import json: %v\n%s", err, raw)
	}
	rewriteImportDoc(&doc, home, stack)
	return doc
}

func normImportDoc(t *testing.T, doc *importDoc, home, stack string) importDoc {
	t.Helper()
	if doc == nil {
		t.Fatal("missing servers document")
	}
	out := *doc
	rewriteImportDoc(&out, home, stack)
	return out
}

func rewriteImportDoc(doc *importDoc, home, stack string) {
	doc.StackFile = rewriteFixturePath(doc.StackFile, home, stack)
	doc.BackupPath = rewriteFixturePath(doc.BackupPath, home, stack)
	for i := range doc.Sources {
		doc.Sources[i].Path = rewriteFixturePath(doc.Sources[i].Path, home, stack)
		doc.Sources[i].AlternatePath = rewriteFixturePath(doc.Sources[i].AlternatePath, home, stack)
		for j := range doc.Sources[i].Notes {
			doc.Sources[i].Notes[j] = rewriteFixturePath(doc.Sources[i].Notes[j], home, stack)
		}
	}
	for i := range doc.Servers {
		s := &doc.Servers[i]
		s.SourcePath = rewriteFixturePath(s.SourcePath, home, stack)
		for j := range s.SourcePaths {
			s.SourcePaths[j] = rewriteFixturePath(s.SourcePaths[j], home, stack)
		}
		for j := range s.Origins {
			s.Origins[j].Path = rewriteFixturePath(s.Origins[j].Path, home, stack)
		}
		for j := range s.Warnings {
			s.Warnings[j] = rewriteFixturePath(s.Warnings[j], home, stack)
		}
	}
}

func rewriteFixturePath(s, home, stack string) string {
	if stack != "" {
		s = strings.ReplaceAll(s, stack, "<STACK>")
	}
	if home != "" {
		s = strings.ReplaceAll(s, home, "<HOME>")
	}
	return importBackupStampRe.ReplaceAllString(s, ".gridctl-backup-TIMESTAMP")
}

func filterEntries(entries []skillImportEntryDoc, kind string) []skillImportEntryDoc {
	out := []skillImportEntryDoc{}
	for _, e := range entries {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func normSkillEntries(entries []skillImportEntryDoc, home string) []skillImportEntryDoc {
	out := append([]skillImportEntryDoc{}, entries...)
	if out == nil {
		out = []skillImportEntryDoc{}
	}
	for i := range out {
		out[i].Location = strings.ReplaceAll(out[i].Location, home, "<HOME>")
		out[i].Reason = strings.ReplaceAll(out[i].Reason, home, "<HOME>")
	}
	return out
}

func TestUnifiedImport_Help(t *testing.T) {
	bin := buildImportBinary(t)
	home := t.TempDir()
	cmd := exec.Command(bin, "--home", home, "import", "--help")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=",
		"GRIDCTL_HOME=",
		"NO_COLOR=1",
		"LANG=C.UTF-8",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("help: %v\n%s", err, out)
	}
	text := string(out)
	for _, want := range []string{
		"With the default --kind, the only file",
		"modified is the stack file",
		"skills:",
		"agents:",
		"context:",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("help missing %q\n%s", want, text)
		}
	}
	root := exec.Command(bin, "--help")
	root.Env = cmd.Env
	rootOut, err := root.CombinedOutput()
	if err != nil {
		t.Fatalf("root help: %v\n%s", err, rootOut)
	}
	if !strings.Contains(string(rootOut), "Import servers, skills, agents, and context from installed clients") {
		t.Fatalf("short missing from command list:\n%s", rootOut)
	}
	if strings.Contains(text, "home unavailable") || strings.Contains(text, "resolving home directory") {
		t.Fatalf("help ignored --home:\n%s", text)
	}
	if strings.Contains(text, "Client configs are read-only: the only file modified") {
		t.Fatal("stale read-only sentence remains")
	}

	skill := exec.Command(bin, "skill", "import", "--help")
	skill.Env = cmd.Env
	skillOut, err := skill.CombinedOutput()
	if err != nil {
		t.Fatalf("skill help: %v\n%s", err, skillOut)
	}
	if !strings.Contains(string(skillOut), "\n     explicitly selected entry skipped\n") {
		t.Fatalf("skill import help indentation:\n%s", skillOut)
	}
}

func TestUnifiedImport_SkillSelectionHint(t *testing.T) {
	bin := buildImportBinary(t)
	home := t.TempDir()
	root := t.TempDir()
	writeOpenCodeImportHome(t, home, "# prefs\n")
	stack := writeStack(t, root)
	stdout, stderr, code := runImportBin(t, bin, home, root, "import", "opencode", "--kind", "skills", "--file", stack)
	if code != 1 {
		t.Fatalf("exit %d stdout=%s stderr=%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "--select") {
		t.Fatalf("unified guidance names --select: %s", stderr)
	}
	if !strings.Contains(stderr, "pass --all or --yes") {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestImportCoverageLines_HomeErrorOnce(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("GRIDCTL_HOME", "")
	t.Setenv("USERPROFILE", "")
	got := importCoverageLines()
	if strings.Count(got, "resolving home directory") != 1 {
		t.Fatalf("home error wrapped twice:\n%s", got)
	}
}
