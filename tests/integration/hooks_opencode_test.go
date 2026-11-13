//go:build integration && !windows

package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const openCodeIfDetail = `dropped if "Bash(git *)": the handler runs on every matched call`

type hookSyncDocRow struct {
	Kind       string `json:"kind"`
	Hook       string `json:"hook"`
	Client     string `json:"client"`
	Channel    string `json:"channel"`
	Target     string `json:"target"`
	Action     string `json:"action"`
	Detail     string `json:"detail"`
	BackupPath string `json:"backup_path"`
	ScriptDir  string `json:"script_dir"`
}

type hookStatusDocRow struct {
	Kind      string `json:"kind"`
	Hook      string `json:"hook"`
	Client    string `json:"client"`
	Channel   string `json:"channel"`
	Render    string `json:"render"`
	State     string `json:"state"`
	Target    string `json:"target"`
	ScriptDir string `json:"script_dir"`
	Detail    string `json:"detail"`
}

func openCodePluginPath(home string) string {
	return filepath.Join(home, ".config", "opencode", "plugins", "gridctl-hook-block-force-push.ts")
}

func openCodeScriptDir(home string) string {
	return filepath.Join(home, ".gridctl", "hooks", "block-force-push@opencode")
}

func claudeScriptDir(home string) string {
	return filepath.Join(home, ".gridctl", "hooks", "block-force-push")
}

// openCodeHookHome imports the hook pack into a home with Claude Code and
// OpenCode initialized.
func openCodeHookHome(t *testing.T, bin string) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	repo = commitPack(t, hookPackFiles("#!/bin/sh\nexit 0\n", ""), map[string]os.FileMode{
		"hooks/block-force-push/scripts/block.sh": 0o755,
	})
	if stdout, stderr, code := runHookCLI(t, bin, home, "pack", "add", repo); code != 0 {
		t.Fatalf("pack add exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, d := range []string{".claude", ".config/opencode"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, repo
}

func hookSyncJSON(t *testing.T, bin, home string, args ...string) ([]hookSyncDocRow, int) {
	t.Helper()
	full := append([]string{"skill", "project", "sync", "--kind", "hook", "--format", "json"}, args...)
	stdout, stderr, code := runHookCLI(t, bin, home, full...)
	var doc struct {
		Results []hookSyncDocRow `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode sync json (exit %d): %v\n%s\n%s", code, err, stdout, stderr)
	}
	return doc.Results, code
}

func hookStatusJSON(t *testing.T, bin, home, client string) (hookStatusDocRow, bool) {
	t.Helper()
	stdout, stderr, _ := runHookCLI(t, bin, home, "skill", "project", "status", "--format", "json")
	var doc struct {
		Projections []hookStatusDocRow `json:"projections"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode status json: %v\n%s\n%s", err, stdout, stderr)
	}
	for _, p := range doc.Projections {
		if p.Kind == "hook" && p.Hook == "block-force-push" && p.Client == client {
			return p, true
		}
	}
	return hookStatusDocRow{}, false
}

func hookUnsyncJSON(t *testing.T, bin, home string, args ...string) []hookSyncDocRow {
	t.Helper()
	full := append([]string{"skill", "project", "unsync", "--kind", "hook", "block-force-push", "--format", "json"}, args...)
	stdout, stderr, code := runHookCLI(t, bin, home, full...)
	if code != 0 {
		t.Fatalf("unsync exit %d\n%s\n%s", code, stdout, stderr)
	}
	var doc struct {
		Results []hookSyncDocRow `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode unsync json: %v\n%s", err, stdout)
	}
	return doc.Results
}

func TestHooksPack_ProjectOpenCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, repo := openCodeHookHome(t, bin)
	plugin := openCodePluginPath(home)

	rows, code := hookSyncJSON(t, bin, home, "--clients", "opencode", "block-force-push")
	if code != 0 || len(rows) != 1 {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	r := rows[0]
	if r.Action != "copied" || r.Channel != "dedicated-file" || r.Target != plugin || r.Detail != openCodeIfDetail || r.ScriptDir != openCodeScriptDir(home) {
		t.Fatalf("sync row = %+v", r)
	}
	info, err := os.Stat(plugin)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("plugin mode = %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(plugin)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`// gridctl hook plugin v1 for hook "block-force-push".`,
		`import { Plugin } from "@opencode/plugin"`,
		`"timeoutMs": 600000`,
		`"script": "` + filepath.Join(openCodeScriptDir(home), "scripts", "block.sh") + `"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plugin is missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(openCodeScriptDir(home), "scripts", "block.sh")); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(home, ".gridctl", "project.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"client: opencode", "channel: dedicated-file", "path: " + plugin, "script_dir: " + openCodeScriptDir(home), "tree_hash: sha256:"} {
		if !strings.Contains(string(lock), want) {
			t.Fatalf("lockfile is missing %q:\n%s", want, lock)
		}
	}

	st, ok := hookStatusJSON(t, bin, home, "opencode")
	if !ok || st.State != "in-sync" || st.Detail != openCodeIfDetail || st.Channel != "dedicated-file" || st.Render != "lossy" {
		t.Fatalf("status = %+v (found %v)", st, ok)
	}

	// A hand edit is drifted, refused on sync, and --force rewrites it
	// after a backup.
	edited := text + "// hand edit\n"
	if err := os.WriteFile(plugin, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, _ := hookStatusJSON(t, bin, home, "opencode"); st.State != "drifted" {
		t.Fatalf("status after edit = %+v", st)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "opencode", "block-force-push")
	if code != 1 || rows[0].Action != "skipped-drift" {
		t.Fatalf("drift sync exit %d rows %+v", code, rows)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "opencode", "--force", "block-force-push")
	if code != 0 || rows[0].Action != "updated" {
		t.Fatalf("forced sync exit %d rows %+v", code, rows)
	}
	backupDir := filepath.Join(home, ".gridctl", "project-backups", "hook-file", "opencode", "block-force-push")
	if !strings.HasPrefix(rows[0].BackupPath, backupDir+string(filepath.Separator)) {
		t.Fatalf("backup path = %q", rows[0].BackupPath)
	}
	if backup, err := os.ReadFile(rows[0].BackupPath); err != nil || string(backup) != edited {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	if again, _ := os.ReadFile(plugin); string(again) != text {
		t.Fatal("--force did not restore the rendered plugin")
	}

	// The claude-code projection owns a separate script copy.
	if _, code := hookSyncJSON(t, bin, home, "--clients", "claude-code", "block-force-push"); code != 0 {
		t.Fatalf("claude-code sync exit %d", code)
	}
	if _, err := os.Stat(claudeScriptDir(home)); err != nil {
		t.Fatal(err)
	}

	// An approved script change synced to opencode only leaves claude-code
	// stale, not drifted, and reset does not keep the hook.
	commitPackChange(t, repo, map[string]string{
		"hooks/block-force-push/scripts/block.sh": "#!/bin/sh\necho approved\nexit 0\n",
	}, map[string]os.FileMode{"hooks/block-force-push/scripts/block.sh": 0o755})
	if stdout, stderr, code := runHookCLI(t, bin, home, "skill", "update", "block-force-push"); code != 0 {
		t.Fatalf("update exit %d\n%s\n%s", code, stdout, stderr)
	}
	if rows, code := hookSyncJSON(t, bin, home, "--clients", "opencode", "block-force-push"); code != 0 || rows[0].Action != "updated" {
		t.Fatalf("opencode resync exit %d rows %+v", code, rows)
	}
	claudeScript, err := os.ReadFile(filepath.Join(claudeScriptDir(home), "scripts", "block.sh"))
	if err != nil || strings.Contains(string(claudeScript), "approved") {
		t.Fatalf("claude-code copy changed: %q %v", claudeScript, err)
	}
	if st, _ := hookStatusJSON(t, bin, home, "claude-code"); st.State != "stale" || !strings.Contains(st.Detail, "registry scripts differ from the synced copy") {
		t.Fatalf("claude-code status = %+v", st)
	}
	dryOut, dryErr, dryCode := runHookCLI(t, bin, home, "reset", "--dry-run", "--format", "json")
	if dryCode != 0 {
		t.Fatalf("reset dry-run exit %d\n%s\n%s", dryCode, dryOut, dryErr)
	}
	if containsString(decodeHookReset(t, dryOut).Kept, "hook/block-force-push") {
		t.Fatalf("reset keeps a stale hook:\n%s", dryOut)
	}

	// Each unsync takes only its own client's file and script copy.
	un := hookUnsyncJSON(t, bin, home, "--clients", "opencode")
	if len(un) != 1 || un[0].Action != "removed" || un[0].Client != "opencode" {
		t.Fatalf("opencode unsync = %+v", un)
	}
	if _, err := os.Stat(plugin); !os.IsNotExist(err) {
		t.Fatalf("plugin remains: %v", err)
	}
	if _, err := os.Stat(openCodeScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("opencode script copy remains: %v", err)
	}
	if _, err := os.Stat(claudeScriptDir(home)); err != nil {
		t.Fatalf("opencode unsync removed the claude-code copy: %v", err)
	}
	if _, ok := hookStatusJSON(t, bin, home, "claude-code"); !ok {
		t.Fatal("opencode unsync purged the claude-code record")
	}

	if _, code := hookSyncJSON(t, bin, home, "--clients", "opencode", "block-force-push"); code != 0 {
		t.Fatalf("opencode re-sync exit %d", code)
	}
	if err := os.Remove(plugin); err != nil {
		t.Fatal(err)
	}
	un = hookUnsyncJSON(t, bin, home, "--clients", "opencode")
	if len(un) != 1 || un[0].Action != "already-gone" {
		t.Fatalf("missing-plugin unsync = %+v", un)
	}
	if _, err := os.Stat(openCodeScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("opencode script copy remains after already-gone: %v", err)
	}

	if _, code := hookSyncJSON(t, bin, home, "--clients", "opencode", "block-force-push"); code != 0 {
		t.Fatalf("opencode re-sync exit %d", code)
	}
	un = hookUnsyncJSON(t, bin, home, "--clients", "claude-code")
	if len(un) != 1 || un[0].Action != "removed" {
		t.Fatalf("claude-code unsync = %+v", un)
	}
	if _, err := os.Stat(claudeScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("claude-code copy remains: %v", err)
	}
	if _, err := os.Stat(openCodeScriptDir(home)); err != nil {
		t.Fatalf("claude-code unsync removed the opencode copy: %v", err)
	}
	if _, err := os.Stat(plugin); err != nil {
		t.Fatalf("claude-code unsync removed the plugin: %v", err)
	}
	if st, _ := hookStatusJSON(t, bin, home, "opencode"); st.State != "in-sync" {
		t.Fatalf("opencode status after claude-code unsync = %+v", st)
	}
}

func TestHooksPack_ProjectOpenCodeUnexpressible(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home := t.TempDir()
	files := hookPackFiles("#!/bin/sh\nexit 0\n", "")
	files["hooks/block-force-push/hook.json"] = strings.Replace(files["hooks/block-force-push/hook.json"], `"PreToolUse"`, `"Stop"`, 1)
	repo := commitPack(t, files, map[string]os.FileMode{"hooks/block-force-push/scripts/block.sh": 0o755})
	if stdout, stderr, code := runHookCLI(t, bin, home, "pack", "add", repo); code != 0 {
		t.Fatalf("pack add exit %d\n%s\n%s", code, stdout, stderr)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	rows, code := hookSyncJSON(t, bin, home, "--clients", "opencode", "block-force-push")
	if code != 1 || len(rows) != 1 || rows[0].Action != "skipped-unexpressible" || rows[0].Detail != `event "Stop" has no opencode mapping` {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	if _, err := os.Stat(openCodePluginPath(home)); !os.IsNotExist(err) {
		t.Fatalf("plugin written: %v", err)
	}
	stdout, stderr, code := runHookCLI(t, bin, home, "pack", "apply", "hook-pack")
	if code != 0 || !strings.Contains(stdout, "skipped-unexpressible") {
		t.Fatalf("pack apply exit %d\n%s\n%s", code, stdout, stderr)
	}
}

type packDoc struct {
	Rows []struct {
		Kind        string `json:"kind"`
		Name        string `json:"name"`
		Client      string `json:"client"`
		Action      string `json:"action"`
		Detail      string `json:"detail"`
		Remediation string `json:"remediation"`
	} `json:"rows"`
	Kept []string `json:"kept"`
}

func TestHooksPack_PackApplyOpenCode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, _ := openCodeHookHome(t, bin)
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runHookCLI(t, bin, home, "pack", "apply", "hook-pack", "--format", "json")
	if code != 0 {
		t.Fatalf("pack apply exit %d\n%s\n%s", code, stdout, stderr)
	}
	var applied packDoc
	if err := json.Unmarshal([]byte(stdout), &applied); err != nil {
		t.Fatalf("decode apply: %v\n%s", err, stdout)
	}
	got := map[string]string{}
	for _, r := range applied.Rows {
		if r.Kind == "hook" {
			got[r.Client] = r.Action
		}
	}
	if got["claude-code"] != "copied" || got["opencode"] != "copied" || got["cursor"] != "skipped-unsupported" {
		t.Fatalf("apply rows = %+v", applied.Rows)
	}
	statusOut, _, _ := runHookCLI(t, bin, home, "pack", "status", "hook-pack", "--format", "json")
	if !strings.Contains(statusOut, `"client": "opencode"`) {
		t.Fatalf("pack status:\n%s", statusOut)
	}

	if err := os.WriteFile(openCodePluginPath(home), []byte("// hand edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = runHookCLI(t, bin, home, "pack", "remove", "hook-pack", "--format", "json")
	if code != 1 {
		t.Fatalf("drift remove exit %d\n%s\n%s", code, stdout, stderr)
	}
	var removed packDoc
	if err := json.Unmarshal([]byte(stdout), &removed); err != nil {
		t.Fatalf("decode remove: %v\n%s", err, stdout)
	}
	if !containsString(removed.Kept, "hook/block-force-push") {
		t.Fatalf("kept = %v", removed.Kept)
	}
	named := false
	for _, r := range removed.Rows {
		if r.Kind == "hook" && r.Action == "skipped-drift" && r.Client == "opencode" && strings.Contains(r.Remediation, "--force") {
			named = true
		}
	}
	if !named {
		t.Fatalf("refusal does not name opencode: %+v", removed.Rows)
	}
	if _, err := os.Stat(filepath.Join(home, ".gridctl", "registry", "hooks", "block-force-push")); err != nil {
		t.Fatalf("registry hook removed despite drift: %v", err)
	}
	if _, err := os.Stat(openCodePluginPath(home)); err != nil {
		t.Fatalf("drifted plugin removed: %v", err)
	}

	stdout, stderr, code = runHookCLI(t, bin, home, "pack", "remove", "hook-pack", "--force")
	if code != 0 {
		t.Fatalf("forced remove exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, p := range []string{openCodePluginPath(home), openCodeScriptDir(home), claudeScriptDir(home), filepath.Join(home, ".gridctl", "registry", "hooks", "block-force-push")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s remains after --force: %v", p, err)
		}
	}
}

func TestHooksPack_ResetRetiresSecondClient(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)

	t.Run("both clients", func(t *testing.T) {
		home, _ := openCodeHookHome(t, bin)
		if _, code := hookSyncJSON(t, bin, home, "block-force-push"); code != 0 {
			t.Fatalf("sync exit %d", code)
		}
		dryOut, _, dryCode := runHookCLI(t, bin, home, "reset", "--dry-run", "--format", "json")
		if dryCode != 0 {
			t.Fatalf("dry-run exit %d\n%s", dryCode, dryOut)
		}
		clients := map[string]string{}
		for _, r := range decodeHookReset(t, dryOut).Rows {
			if r.Kind == "hook" && r.Action == "would-remove" {
				clients[r.Client] = r.ScriptDir
			}
		}
		if clients["claude-code"] != claudeScriptDir(home) || clients["opencode"] != openCodeScriptDir(home) {
			t.Fatalf("dry-run hook rows = %v\n%s", clients, dryOut)
		}
		stdout, stderr, code := runHookCLI(t, bin, home, "reset", "--yes", "--format", "json")
		if code != 0 {
			t.Fatalf("reset exit %d\n%s\n%s", code, stdout, stderr)
		}
		removed := map[string]bool{}
		for _, r := range decodeHookReset(t, stdout).Rows {
			if r.Kind == "hook" && r.Action == "removed" {
				removed[r.Client] = true
			}
		}
		if !removed["claude-code"] || !removed["opencode"] {
			t.Fatalf("reset rows:\n%s", stdout)
		}
		for _, p := range []string{openCodePluginPath(home), openCodeScriptDir(home), claudeScriptDir(home)} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("%s remains after reset: %v", p, err)
			}
		}
	})

	t.Run("drift on the second client", func(t *testing.T) {
		home, _ := openCodeHookHome(t, bin)
		if _, code := hookSyncJSON(t, bin, home, "block-force-push"); code != 0 {
			t.Fatalf("sync exit %d", code)
		}
		if err := os.WriteFile(openCodePluginPath(home), []byte("// hand edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, code := runHookCLI(t, bin, home, "reset", "--yes", "--format", "json")
		if code != 1 {
			t.Fatalf("drift reset exit %d\n%s\n%s", code, stdout, stderr)
		}
		doc := decodeHookReset(t, stdout)
		if !containsString(doc.Kept, "hook/block-force-push") {
			t.Fatalf("kept = %v", doc.Kept)
		}
		// Reset keys kept hooks by name: drift on one client keeps both.
		for _, p := range []string{openCodePluginPath(home), openCodeScriptDir(home), claudeScriptDir(home)} {
			if _, err := os.Stat(p); err != nil {
				t.Fatalf("%s removed despite drift: %v", p, err)
			}
		}
		forced, forcedErr, forcedCode := runHookCLI(t, bin, home, "reset", "--yes", "--force", "--format", "json")
		if forcedCode != 0 {
			t.Fatalf("force reset exit %d\n%s\n%s", forcedCode, forced, forcedErr)
		}
		for _, p := range []string{openCodePluginPath(home), openCodeScriptDir(home), claudeScriptDir(home)} {
			if _, err := os.Stat(p); !os.IsNotExist(err) {
				t.Fatalf("%s remains after --force: %v", p, err)
			}
		}
	})
}
