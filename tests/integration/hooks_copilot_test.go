//go:build integration && !windows

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func copilotFilePath(home string) string {
	return filepath.Join(home, ".copilot", "hooks", "gridctl-block-force-push.json")
}

func copilotScriptDir(home string) string {
	return filepath.Join(home, ".gridctl", "hooks", "block-force-push@copilot")
}

// copilotHookHome imports the hook pack into a home with Claude Code and
// Copilot CLI initialized.
func copilotHookHome(t *testing.T, bin string) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	repo = commitPack(t, hookPackFiles("#!/bin/sh\nexit 0\n", ""), map[string]os.FileMode{
		"hooks/block-force-push/scripts/block.sh": 0o755,
	})
	if stdout, stderr, code := runHookCLI(t, bin, home, "pack", "add", repo); code != 0 {
		t.Fatalf("pack add exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, d := range []string{".claude", ".copilot"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, repo
}

type copilotHandlerDoc struct {
	Type       string   `json:"type"`
	Exec       string   `json:"exec"`
	Args       []string `json:"args"`
	Matcher    *string  `json:"matcher"`
	TimeoutSec *int     `json:"timeoutSec"`
	Command    *string  `json:"command"`
	Bash       *string  `json:"bash"`
}

type copilotFileDoc struct {
	Version int                            `json:"version"`
	Hooks   map[string][]copilotHandlerDoc `json:"hooks"`
}

func readCopilotFile(t *testing.T, path string) (copilotFileDoc, []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc copilotFileDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, data)
	}
	return doc, data
}

func TestHooksPack_ProjectCopilot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, repo := copilotHookHome(t, bin)
	file := copilotFilePath(home)

	rows, code := hookSyncJSON(t, bin, home, "--clients", "copilot", "block-force-push")
	if code != 0 || len(rows) != 1 {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	r := rows[0]
	if r.Action != "copied" || r.Channel != "dedicated-file" || r.Target != file || r.Detail != openCodeIfDetail || r.ScriptDir != copilotScriptDir(home) {
		t.Fatalf("sync row = %+v", r)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("file mode = %v", info.Mode().Perm())
	}
	doc, data := readCopilotFile(t, file)
	text := string(data)
	if doc.Version != 1 || len(doc.Hooks) != 1 || len(doc.Hooks["PreToolUse"]) != 1 {
		t.Fatalf("file = %s", text)
	}
	h := doc.Hooks["PreToolUse"][0]
	script := filepath.Join(copilotScriptDir(home), "scripts", "block.sh")
	if h.Type != "command" || h.Exec != script || h.Args == nil || len(h.Args) != 0 || h.Matcher == nil || *h.Matcher != "Bash" || h.TimeoutSec != nil || h.Command != nil || h.Bash != nil {
		t.Fatalf("handler = %+v\n%s", h, text)
	}
	if !strings.HasSuffix(text, "}\n") || !strings.Contains(text, "\n  \"version\": 1,\n") {
		t.Fatalf("file is not two-space indented with a trailing newline:\n%s", text)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(home, ".gridctl", "project.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"client: copilot", "channel: dedicated-file", "path: " + file, "script_dir: " + copilotScriptDir(home), "tree_hash: sha256:"} {
		if !strings.Contains(string(lock), want) {
			t.Fatalf("lockfile is missing %q:\n%s", want, lock)
		}
	}

	st, ok := hookStatusJSON(t, bin, home, "copilot")
	if !ok || st.State != "in-sync" || st.Detail != openCodeIfDetail || st.Channel != "dedicated-file" || st.Render != "lossy" {
		t.Fatalf("status = %+v (found %v)", st, ok)
	}

	// A hand edit is drifted, refused on sync, and --force rewrites it
	// after a backup.
	edited := strings.Replace(text, `"matcher": "Bash"`, `"matcher": "Read"`, 1)
	if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, _ := hookStatusJSON(t, bin, home, "copilot"); st.State != "drifted" {
		t.Fatalf("status after edit = %+v", st)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "copilot", "block-force-push")
	if code != 1 || rows[0].Action != "skipped-drift" {
		t.Fatalf("drift sync exit %d rows %+v", code, rows)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "copilot", "--force", "block-force-push")
	if code != 0 || rows[0].Action != "updated" {
		t.Fatalf("forced sync exit %d rows %+v", code, rows)
	}
	backupDir := filepath.Join(home, ".gridctl", "project-backups", "hook-file", "copilot", "block-force-push")
	if !strings.HasPrefix(rows[0].BackupPath, backupDir+string(filepath.Separator)) {
		t.Fatalf("backup path = %q", rows[0].BackupPath)
	}
	if backup, err := os.ReadFile(rows[0].BackupPath); err != nil || string(backup) != edited {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	if again, _ := os.ReadFile(file); string(again) != text {
		t.Fatal("--force did not restore the rendered file")
	}

	// The claude-code projection owns a separate script copy.
	if _, code := hookSyncJSON(t, bin, home, "--clients", "claude-code", "block-force-push"); code != 0 {
		t.Fatalf("claude-code sync exit %d", code)
	}
	if _, err := os.Stat(claudeScriptDir(home)); err != nil {
		t.Fatal(err)
	}

	// An approved script change synced to copilot only leaves claude-code
	// stale, not drifted, and reset does not keep the hook.
	commitPackChange(t, repo, map[string]string{
		"hooks/block-force-push/scripts/block.sh": "#!/bin/sh\necho approved\nexit 0\n",
	}, map[string]os.FileMode{"hooks/block-force-push/scripts/block.sh": 0o755})
	if stdout, stderr, code := runHookCLI(t, bin, home, "skill", "update", "block-force-push"); code != 0 {
		t.Fatalf("update exit %d\n%s\n%s", code, stdout, stderr)
	}
	if rows, code := hookSyncJSON(t, bin, home, "--clients", "copilot", "block-force-push"); code != 0 || rows[0].Action != "updated" {
		t.Fatalf("copilot resync exit %d rows %+v", code, rows)
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
	un := hookUnsyncJSON(t, bin, home, "--clients", "copilot")
	if len(un) != 1 || un[0].Action != "removed" || un[0].Client != "copilot" {
		t.Fatalf("copilot unsync = %+v", un)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("hook file remains: %v", err)
	}
	if _, err := os.Stat(copilotScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("copilot script copy remains: %v", err)
	}
	if _, err := os.Stat(claudeScriptDir(home)); err != nil {
		t.Fatalf("copilot unsync removed the claude-code copy: %v", err)
	}
	if _, ok := hookStatusJSON(t, bin, home, "claude-code"); !ok {
		t.Fatal("copilot unsync purged the claude-code record")
	}

	if _, code := hookSyncJSON(t, bin, home, "--clients", "copilot", "block-force-push"); code != 0 {
		t.Fatalf("copilot re-sync exit %d", code)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	un = hookUnsyncJSON(t, bin, home, "--clients", "copilot")
	if len(un) != 1 || un[0].Action != "already-gone" {
		t.Fatalf("missing-file unsync = %+v", un)
	}
	if _, err := os.Stat(copilotScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("copilot script copy remains after already-gone: %v", err)
	}

	// A file gridctl did not record is foreign until --force claims it
	// after a backup.
	if err := os.WriteFile(file, []byte(`{"version": 1, "hooks": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "copilot", "block-force-push")
	if code != 1 || rows[0].Action != "skipped-foreign" || !strings.Contains(rows[0].Detail, file) {
		t.Fatalf("foreign sync exit %d rows %+v", code, rows)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "copilot", "--force", "block-force-push")
	if code != 0 || rows[0].Action != "updated" || rows[0].BackupPath == "" {
		t.Fatalf("forced claim exit %d rows %+v", code, rows)
	}

	un = hookUnsyncJSON(t, bin, home, "--clients", "claude-code")
	if len(un) != 1 || un[0].Action != "removed" {
		t.Fatalf("claude-code unsync = %+v", un)
	}
	if _, err := os.Stat(claudeScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("claude-code copy remains: %v", err)
	}
	if _, err := os.Stat(copilotScriptDir(home)); err != nil {
		t.Fatalf("claude-code unsync removed the copilot copy: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("claude-code unsync removed the hook file: %v", err)
	}
	if st, _ := hookStatusJSON(t, bin, home, "copilot"); st.State != "in-sync" {
		t.Fatalf("copilot status after claude-code unsync = %+v", st)
	}
}

func TestHooksPack_ProjectCopilotUnexpressible(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(home, ".copilot"), 0o755); err != nil {
		t.Fatal(err)
	}
	rows, code := hookSyncJSON(t, bin, home, "--clients", "copilot", "block-force-push")
	if code != 1 || len(rows) != 1 || rows[0].Action != "skipped-unexpressible" || rows[0].Detail != `event "Stop" has no copilot mapping` {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	if _, err := os.Stat(copilotFilePath(home)); !os.IsNotExist(err) {
		t.Fatalf("hook file written: %v", err)
	}
	stdout, stderr, code := runHookCLI(t, bin, home, "pack", "apply", "hook-pack")
	if code != 0 || !strings.Contains(stdout, "skipped-unexpressible") {
		t.Fatalf("pack apply exit %d\n%s\n%s", code, stdout, stderr)
	}
}

func TestHooksPack_PackApplyCopilot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, _ := copilotHookHome(t, bin)
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
	if got["claude-code"] != "copied" || got["copilot"] != "copied" {
		t.Fatalf("apply rows = %+v", applied.Rows)
	}
	if _, listed := got["opencode"]; listed {
		t.Fatalf("an undetected target produced a row: %+v", applied.Rows)
	}

	if err := os.WriteFile(copilotFilePath(home), []byte("{}\n"), 0o644); err != nil {
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
		if r.Kind == "hook" && r.Action == "skipped-drift" && r.Client == "copilot" && strings.Contains(r.Remediation, "--force") {
			named = true
		}
	}
	if !named {
		t.Fatalf("refusal does not name copilot: %+v", removed.Rows)
	}
	if _, err := os.Stat(copilotFilePath(home)); err != nil {
		t.Fatalf("drifted hook file removed: %v", err)
	}

	stdout, stderr, code = runHookCLI(t, bin, home, "pack", "remove", "hook-pack", "--force")
	if code != 0 {
		t.Fatalf("forced remove exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, p := range []string{copilotFilePath(home), copilotScriptDir(home), claudeScriptDir(home), filepath.Join(home, ".gridctl", "registry", "hooks", "block-force-push")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s remains after --force: %v", p, err)
		}
	}
}

func TestHooksPack_ResetRetiresCopilot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, _ := copilotHookHome(t, bin)
	if _, code := hookSyncJSON(t, bin, home, "block-force-push"); code != 0 {
		t.Fatalf("sync exit %d", code)
	}
	if err := os.WriteFile(copilotFilePath(home), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runHookCLI(t, bin, home, "reset", "--yes", "--format", "json")
	if code != 1 {
		t.Fatalf("drift reset exit %d\n%s\n%s", code, stdout, stderr)
	}
	if !containsString(decodeHookReset(t, stdout).Kept, "hook/block-force-push") {
		t.Fatalf("reset did not keep the drifted hook:\n%s", stdout)
	}
	for _, p := range []string{copilotFilePath(home), copilotScriptDir(home), claudeScriptDir(home)} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s removed despite drift: %v", p, err)
		}
	}
	stdout, stderr, code = runHookCLI(t, bin, home, "reset", "--yes", "--force", "--format", "json")
	if code != 0 {
		t.Fatalf("force reset exit %d\n%s\n%s", code, stdout, stderr)
	}
	removed := map[string]bool{}
	for _, r := range decodeHookReset(t, stdout).Rows {
		if r.Kind == "hook" && r.Action == "removed" {
			removed[r.Client] = true
		}
	}
	if !removed["claude-code"] || !removed["copilot"] {
		t.Fatalf("reset rows:\n%s", stdout)
	}
	for _, p := range []string{copilotFilePath(home), copilotScriptDir(home), claudeScriptDir(home)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s remains after --force: %v", p, err)
		}
	}
}

// TestHooksPack_CopilotExamplePack imports the shipped example hook,
// projects it to Copilot CLI, and runs the projected handler the way the
// file tells Copilot to: exec plus args, no shell, a Claude-shaped
// payload on stdin. The test runs the script; gridctl never does.
func TestHooksPack_CopilotExamplePack(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	hookDir := filepath.Join(hookRepoRoot(t), "examples", "portable-pack", "hooks", "block-force-push")
	definition, err := os.ReadFile(filepath.Join(hookDir, "hook.json"))
	if err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile(filepath.Join(hookDir, "scripts", "block-force-push.sh"))
	if err != nil {
		t.Fatal(err)
	}
	files := hookPackFiles("", "")
	delete(files, "hooks/block-force-push/scripts/block.sh")
	files["hooks/block-force-push/hook.json"] = string(definition)
	files["hooks/block-force-push/scripts/block-force-push.sh"] = string(example)
	repo := commitPack(t, files, map[string]os.FileMode{"hooks/block-force-push/scripts/block-force-push.sh": 0o755})

	home := t.TempDir()
	if stdout, stderr, code := runHookCLI(t, bin, home, "pack", "add", repo); code != 0 {
		t.Fatalf("pack add exit %d\n%s\n%s", code, stdout, stderr)
	}
	if err := os.MkdirAll(filepath.Join(home, ".copilot"), 0o755); err != nil {
		t.Fatal(err)
	}
	rows, code := hookSyncJSON(t, bin, home, "--clients", "copilot", "block-force-push")
	if code != 0 || len(rows) != 1 || rows[0].Action != "copied" {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	doc, _ := readCopilotFile(t, copilotFilePath(home))
	h := doc.Hooks["PreToolUse"][0]
	if copied, err := os.ReadFile(h.Exec); err != nil || !bytes.Equal(copied, example) {
		t.Fatalf("exec %s does not hold the pinned example bytes: %v", h.Exec, err)
	}

	const reason = "git push --force is blocked by the network-eng pack"
	run := func(command string) (int, string, string) {
		payload, err := json.Marshal(map[string]any{
			"hook_event_name": "PreToolUse",
			"session_id":      "s",
			"cwd":             home,
			"tool_name":       "Bash",
			"tool_input":      map[string]string{"command": command},
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, h.Exec, h.Args...)
		cmd.Stdin = bytes.NewReader(payload)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		var exitErr *exec.ExitError
		switch {
		case err == nil:
			return 0, stdout.String(), stderr.String()
		case errors.As(err, &exitErr):
			return exitErr.ExitCode(), stdout.String(), stderr.String()
		default:
			t.Fatal(err)
			return 0, "", ""
		}
	}

	status, stdout, stderr := run("git push origin main " + "--" + "force")
	if status != 2 || strings.TrimSpace(stderr) != reason {
		t.Fatalf("deny exit %d stderr %q", status, stderr)
	}
	var deny struct {
		Decision           string `json:"decision"`
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &deny); err != nil || deny.Decision != "deny" || deny.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("deny stdout %q: %v", stdout, err)
	}
	status, stdout, stderr = run("git push origin main")
	if status != 0 || stdout != "" || stderr != "" {
		t.Fatalf("allow exit %d stdout %q stderr %q", status, stdout, stderr)
	}
}
