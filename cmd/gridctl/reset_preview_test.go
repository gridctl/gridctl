package main

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/resetops"
)

func TestRenderResetPreview_SkippedNotRecreatable(t *testing.T) {
	prev := resetVerbose
	t.Cleanup(func() { resetVerbose = prev })

	hookDetail := "settings file does not parse; reset will report this projection as failed (repair or remove the file, then re-run)"
	daemonDetail := "daemon on port 8180 runs under home /other; not touched (use 'gridctl stop --force' from that home)"
	keptDetail := "a projection of this hook was hand-edited; kept (re-run with --force to remove)"

	doc := &resetops.Doc{
		Home: "/home/user",
		Rows: []resetops.Row{
			{Kind: "hook", Name: "ok-hook", Client: "claude-code", Path: "/settings.json", Action: resetops.ActionWouldRemove},
			{Kind: "hook", Name: "broken", Client: "claude-code", Path: "/settings.json", Action: resetops.ActionSkipped, Detail: hookDetail},
			{Kind: "hook", Name: "edited", Action: resetops.ActionKeptDrift, Detail: keptDetail},
			{Kind: "daemon", Name: "gridctl (other home)", Action: resetops.ActionSkipped, Detail: daemonDetail},
		},
	}
	mgrs := &resetops.Managers{Home: "/home/user"}

	t.Run("summary", func(t *testing.T) {
		resetVerbose = false
		out := renderPreview(t, mgrs, doc)

		hookCount, ok := recreatableCount(out, "hook")
		if !ok || hookCount != 1 {
			t.Fatalf("recreatable hook count = %d, ok=%v\n%s", hookCount, ok, out)
		}
		if _, found := recreatableCount(out, "daemon"); found {
			t.Fatalf("daemon skipped row counted as recreatable\n%s", out)
		}
		for _, want := range []string{
			"across clients: claude-code 1",
			"SKIPPED (not removed) (2)",
			"hook broken: " + hookDetail,
			"daemon gridctl (other home): " + daemonDetail,
			"KEPT (YOUR EDITS ARE SAFE) (1)",
			"hook edited: " + keptDetail,
			"1 items.",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %q in\n%s", want, out)
			}
		}
		if strings.Contains(out, "FULL LISTING") {
			t.Fatalf("summary must not print the verbose table\n%s", out)
		}
	})

	t.Run("verbose", func(t *testing.T) {
		resetVerbose = true
		out := renderPreview(t, mgrs, doc)
		if !strings.Contains(out, "FULL LISTING") || !strings.Contains(out, "ok-hook") {
			t.Fatalf("verbose listing missing the would-remove row\n%s", out)
		}
		if strings.Contains(out, "broken") && strings.Count(out, "broken") != 1 {
			t.Fatalf("skipped hook listed outside the SKIPPED section\n%s", out)
		}
		if strings.Contains(out, "1 items.") {
			t.Fatalf("verbose output should not also print the item count\n%s", out)
		}
	})

	t.Run("only skipped", func(t *testing.T) {
		resetVerbose = false
		only := &resetops.Doc{
			Home: doc.Home,
			Rows: []resetops.Row{
				{Kind: "hook", Name: "broken", Client: "claude-code", Action: resetops.ActionSkipped, Detail: hookDetail},
			},
		}
		out := renderPreview(t, mgrs, only)
		if !strings.Contains(out, "nothing") {
			t.Fatalf("recreatable section should be empty\n%s", out)
		}
		if _, found := recreatableCount(out, "hook"); found {
			t.Fatalf("skipped hook counted as recreatable\n%s", out)
		}
		if strings.Contains(out, "across clients:") {
			t.Fatalf("skipped client counted in the summary\n%s", out)
		}
		if !strings.Contains(out, "SKIPPED (not removed) (1)") || !strings.Contains(out, "hook broken: "+hookDetail) {
			t.Fatalf("skipped section missing\n%s", out)
		}
		if strings.Contains(out, "items.") {
			t.Fatalf("skipped-only preview should not offer a removal listing\n%s", out)
		}
	})
}

func TestResetLong_PurgeKeepsHandEditedHookGroups(t *testing.T) {
	long := resetCmd.Long
	for _, want := range []string{
		"after in-sync hook groups have been retired from Claude Code settings",
		"hand-edited groups are kept unless --force",
	} {
		if !strings.Contains(long, want) {
			t.Fatalf("reset Long missing %q\n%s", want, long)
		}
	}
	if strings.Contains(long, "after those hook groups have been retired") {
		t.Fatalf("reset Long still claims every hook group is retired before purge\n%s", long)
	}
}

func renderPreview(t *testing.T, mgrs *resetops.Managers, doc *resetops.Doc) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "preview")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	renderResetPreview(f, mgrs, doc)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func recreatableCount(out, kind string) (int, bool) {
	in := false
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "RECREATABLE") {
			in = true
			continue
		}
		if !in {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "REMOVED") || strings.HasPrefix(trimmed, "across clients:") {
			break
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 2 && fields[0] == kind {
			n, err := strconv.Atoi(fields[1])
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}
