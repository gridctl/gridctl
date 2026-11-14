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

const antigravityKey = "gridctl-block-force-push"

func antigravityFilePath(home string) string {
	return filepath.Join(home, ".gemini", "config", "hooks.json")
}

func antigravityScriptDir(home string) string {
	return filepath.Join(home, ".gridctl", "hooks", "block-force-push@antigravity")
}

// antigravityUserFile is a hooks file the user wrote: comments, a sibling
// hook definition, and a sibling non-hook key.
const antigravityUserFile = `// personal hooks
{
  // audit every tool call
  "audit": {
    "enabled": true,
    "PreToolUse": [{"matcher": "*", "hooks": [{"type": "command", "command": "audit.sh"}]}] // inline
  },
  "notes": {"owner": "me"}
}
`

// antigravityHookHome imports the hook pack into a home with Claude Code
// and Antigravity initialized.
func antigravityHookHome(t *testing.T, bin string) (home, repo string) {
	t.Helper()
	home = t.TempDir()
	repo = commitPack(t, hookPackFiles("#!/bin/sh\nexit 0\n", ""), map[string]os.FileMode{
		"hooks/block-force-push/scripts/block.sh": 0o755,
	})
	if stdout, stderr, code := runHookCLI(t, bin, home, "pack", "add", repo); code != 0 {
		t.Fatalf("pack add exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, d := range []string{".claude", ".gemini/config"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, repo
}

type antigravityHandlerDoc struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

type antigravityGroupDoc struct {
	Matcher string                  `json:"matcher"`
	Hooks   []antigravityHandlerDoc `json:"hooks"`
}

type antigravityKeyDoc struct {
	Enabled     bool                  `json:"enabled"`
	PreToolUse  []antigravityGroupDoc `json:"PreToolUse"`
	PostToolUse []antigravityGroupDoc `json:"PostToolUse"`
}

// readAntigravityKeys decodes the hooks file after stripping the lines
// that are only comments and the trailing comments the fixtures use.
func readAntigravityKeys(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if i := strings.Index(line, "] // "); i >= 0 {
			line = line[:i+1]
		}
		lines = append(lines, line)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.Join(lines, "\n")), &doc); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, data)
	}
	return doc
}

func antigravityKeyValue(t *testing.T, home string) (antigravityKeyDoc, bool) {
	t.Helper()
	raw, ok := readAntigravityKeys(t, antigravityFilePath(home))[antigravityKey]
	if !ok {
		return antigravityKeyDoc{}, false
	}
	var v antigravityKeyDoc
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, true
}

func TestHooksPack_ProjectAntigravity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, _ := antigravityHookHome(t, bin)
	file := antigravityFilePath(home)
	if err := os.WriteFile(file, []byte(antigravityUserFile), 0o644); err != nil {
		t.Fatal(err)
	}

	rows, code := hookSyncJSON(t, bin, home, "--clients", "antigravity", "block-force-push")
	if code != 0 || len(rows) != 1 {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	r := rows[0]
	if r.Action != "copied" || r.Channel != "merge-key" || r.Target != file || r.Detail != openCodeIfDetail || r.ScriptDir != antigravityScriptDir(home) {
		t.Fatalf("sync row = %+v", r)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, strings.TrimSuffix(antigravityUserFile, "\n}\n")) {
		t.Fatalf("sibling keys or comments changed:\n%s", text)
	}
	keys := readAntigravityKeys(t, file)
	if _, ok := keys["audit"]; !ok {
		t.Fatalf("sibling hook lost:\n%s", text)
	}
	if _, ok := keys["notes"]; !ok {
		t.Fatalf("sibling key lost:\n%s", text)
	}
	v, ok := antigravityKeyValue(t, home)
	script := filepath.Join(antigravityScriptDir(home), "scripts", "block.sh")
	if !ok || !v.Enabled || len(v.PreToolUse) != 1 || len(v.PostToolUse) != 0 {
		t.Fatalf("key = %+v\n%s", v, text)
	}
	g := v.PreToolUse[0]
	if g.Matcher != "run_command" || len(g.Hooks) != 1 || g.Hooks[0].Type != "command" || g.Hooks[0].Command != "'"+script+"'" || g.Hooks[0].Timeout != 600 {
		t.Fatalf("group = %+v", g)
	}
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}
	lock, err := os.ReadFile(filepath.Join(home, ".gridctl", "project.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"client: antigravity", "channel: merge-key", "script_dir: " + antigravityScriptDir(home), "tree_hash: sha256:"} {
		if !strings.Contains(string(lock), want) {
			t.Fatalf("lockfile is missing %q:\n%s", want, lock)
		}
	}

	st, ok := hookStatusJSON(t, bin, home, "antigravity")
	if !ok || st.State != "in-sync" || st.Detail != openCodeIfDetail || st.Channel != "merge-key" || st.Render != "lossy" || st.Target != file {
		t.Fatalf("status = %+v (found %v)", st, ok)
	}

	// A hand-edited key is drifted, refused on sync, and --force rewrites
	// it after a backup.
	edited := strings.Replace(text, `"matcher": "run_command"`, `"matcher": "view_file"`, 1)
	if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if st, _ := hookStatusJSON(t, bin, home, "antigravity"); st.State != "drifted" {
		t.Fatalf("status after edit = %+v", st)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "antigravity", "block-force-push")
	if code != 1 || rows[0].Action != "skipped-drift" {
		t.Fatalf("drift sync exit %d rows %+v", code, rows)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "antigravity", "--force", "block-force-push")
	if code != 0 || rows[0].Action != "updated" || rows[0].BackupPath == "" {
		t.Fatalf("forced sync exit %d rows %+v", code, rows)
	}
	if backup, err := os.ReadFile(rows[0].BackupPath); err != nil || string(backup) != edited {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	if again, _ := os.ReadFile(file); string(again) != text {
		t.Fatalf("--force did not restore the rendered key:\n%s", again)
	}

	// The claude-code projection owns a separate script copy.
	if _, code := hookSyncJSON(t, bin, home, "--clients", "claude-code", "block-force-push"); code != 0 {
		t.Fatalf("claude-code sync exit %d", code)
	}

	// Unsync removes only the gridctl key: the user's file comes back
	// byte for byte, and the claude-code copy stays.
	un := hookUnsyncJSON(t, bin, home, "--clients", "antigravity")
	if len(un) != 1 || un[0].Action != "removed" || un[0].Client != "antigravity" {
		t.Fatalf("antigravity unsync = %+v", un)
	}
	if got, _ := os.ReadFile(file); string(got) != antigravityUserFile {
		t.Fatalf("unsync did not leave the siblings as they were:\n%s", got)
	}
	if _, err := os.Stat(antigravityScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("antigravity script copy remains: %v", err)
	}
	if _, err := os.Stat(claudeScriptDir(home)); err != nil {
		t.Fatalf("antigravity unsync removed the claude-code copy: %v", err)
	}
	if st, ok := hookStatusJSON(t, bin, home, "claude-code"); !ok || st.State != "in-sync" {
		t.Fatalf("claude-code status after antigravity unsync = %+v (found %v)", st, ok)
	}

	// A gridctl-<name> key gridctl did not record is foreign until --force
	// claims it after a backup.
	foreign := strings.Replace(antigravityUserFile, `"notes"`, `"`+antigravityKey+`": {"enabled": false},
  "notes"`, 1)
	if err := os.WriteFile(file, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "antigravity", "block-force-push")
	if code != 1 || rows[0].Action != "skipped-foreign" || !strings.Contains(rows[0].Detail, antigravityKey) || !strings.Contains(rows[0].Detail, "--force") {
		t.Fatalf("foreign sync exit %d rows %+v", code, rows)
	}
	if got, _ := os.ReadFile(file); string(got) != foreign {
		t.Fatal("a foreign key was changed without --force")
	}
	rows, code = hookSyncJSON(t, bin, home, "--clients", "antigravity", "--force", "block-force-push")
	if code != 0 || rows[0].Action != "updated" || rows[0].BackupPath == "" {
		t.Fatalf("forced claim exit %d rows %+v", code, rows)
	}
	if backup, err := os.ReadFile(rows[0].BackupPath); err != nil || string(backup) != foreign {
		t.Fatalf("claim backup = %q, %v", backup, err)
	}
	if v, ok := antigravityKeyValue(t, home); !ok || !v.Enabled || len(v.PreToolUse) != 1 {
		t.Fatalf("claimed key = %+v", v)
	}
	if st, _ := hookStatusJSON(t, bin, home, "antigravity"); st.State != "in-sync" {
		t.Fatalf("status after claim = %+v", st)
	}

	// A file hujson cannot parse is unmanaged, with or without --force.
	if err := os.WriteFile(file, []byte("{ \"audit\": "), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--clients", "antigravity"}, {"--clients", "antigravity", "--force"}} {
		rows, code = hookSyncJSON(t, bin, home, append(args, "block-force-push")...)
		if code != 1 || rows[0].Action != "skipped-unmanaged" {
			t.Fatalf("unparsable sync %v exit %d rows %+v", args, code, rows)
		}
	}
	if got, _ := os.ReadFile(file); string(got) != "{ \"audit\": " {
		t.Fatal("an unparsable file was rewritten")
	}
	un = hookUnsyncJSON(t, bin, home, "--clients", "antigravity", "--force")
	if len(un) != 1 || un[0].Action != "skipped-unmanaged" {
		t.Fatalf("unparsable unsync = %+v", un)
	}
	if got, _ := os.ReadFile(file); string(got) != "{ \"audit\": " {
		t.Fatal("an unparsable file was rewritten by unsync")
	}
}

func TestHooksPack_ProjectAntigravityCreatesFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, _ := antigravityHookHome(t, bin)
	file := antigravityFilePath(home)
	rows, code := hookSyncJSON(t, bin, home, "--clients", "antigravity", "block-force-push")
	if code != 0 || rows[0].Action != "copied" {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	keys := readAntigravityKeys(t, file)
	if _, ok := keys[antigravityKey]; !ok || len(keys) != 1 {
		t.Fatalf("created file keys = %v", keys)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("file mode = %v, %v", info, err)
	}
	lock, err := os.ReadFile(filepath.Join(home, ".gridctl", "project.lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lock), "created_file: true") {
		t.Fatalf("created_file not recorded:\n%s", lock)
	}
	un := hookUnsyncJSON(t, bin, home, "--clients", "antigravity")
	if len(un) != 1 || un[0].Action != "removed" {
		t.Fatalf("unsync = %+v", un)
	}
	if got, _ := os.ReadFile(file); string(got) != "{}\n" {
		t.Fatalf("emptied file = %q", got)
	}
}

func TestHooksPack_ProjectAntigravityUnquotable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home := t.TempDir()
	files := hookPackFiles("#!/bin/sh\nexit 0\n", "")
	files["hooks/block-force-push/hook.json"] = strings.Replace(files["hooks/block-force-push/hook.json"], `"args": []`, `"args": ["it's"]`, 1)
	repo := commitPack(t, files, map[string]os.FileMode{"hooks/block-force-push/scripts/block.sh": 0o755})
	if stdout, stderr, code := runHookCLI(t, bin, home, "pack", "add", repo); code != 0 {
		t.Fatalf("pack add exit %d\n%s\n%s", code, stdout, stderr)
	}
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runHookCLI(t, bin, home, "skill", "project", "sync", "--kind", "hook", "--clients", "antigravity", "--format", "json", "block-force-push")
	var doc struct {
		Results []struct {
			Action string `json:"action"`
			Error  string `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("decode sync: %v\n%s\n%s", err, stdout, stderr)
	}
	if code != 1 || len(doc.Results) != 1 || doc.Results[0].Action != "error" || !strings.Contains(doc.Results[0].Error, `"it's"`) {
		t.Fatalf("sync exit %d\n%s\n%s", code, stdout, stderr)
	}
	for _, p := range []string{antigravityFilePath(home), antigravityScriptDir(home)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s written for an unquotable hook: %v", p, err)
		}
	}
}

func TestHooksPack_PackApplyAntigravity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, _ := antigravityHookHome(t, bin)
	file := antigravityFilePath(home)
	if err := os.WriteFile(file, []byte(antigravityUserFile), 0o644); err != nil {
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
	if got["claude-code"] != "copied" || got["antigravity"] != "copied" {
		t.Fatalf("apply rows = %+v", applied.Rows)
	}
	if _, listed := got["gemini"]; listed {
		t.Fatalf("an Antigravity-only ~/.gemini produced a Gemini CLI row: %+v", applied.Rows)
	}

	text, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(text), `"timeout": 600`, `"timeout": 1`, 1)
	if err := os.WriteFile(file, []byte(edited), 0o644); err != nil {
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
		if r.Kind == "hook" && r.Action == "skipped-drift" && r.Client == "antigravity" && strings.Contains(r.Remediation, "--force") {
			named = true
		}
	}
	if !named {
		t.Fatalf("refusal does not name antigravity: %+v", removed.Rows)
	}
	if got, _ := os.ReadFile(file); string(got) != edited {
		t.Fatal("a drifted key was removed")
	}

	stdout, stderr, code = runHookCLI(t, bin, home, "pack", "remove", "hook-pack", "--force")
	if code != 0 {
		t.Fatalf("forced remove exit %d\n%s\n%s", code, stdout, stderr)
	}
	if got, _ := os.ReadFile(file); string(got) != antigravityUserFile {
		t.Fatalf("forced remove did not leave the siblings as they were:\n%s", got)
	}
	for _, p := range []string{antigravityScriptDir(home), claudeScriptDir(home), filepath.Join(home, ".gridctl", "registry", "hooks", "block-force-push")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s remains after --force: %v", p, err)
		}
	}
}

func TestHooksPack_ResetRetiresAntigravity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	bin := hookCLI(t)
	home, _ := antigravityHookHome(t, bin)
	if err := os.WriteFile(antigravityFilePath(home), []byte(antigravityUserFile), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := hookSyncJSON(t, bin, home, "block-force-push"); code != 0 {
		t.Fatalf("sync exit %d", code)
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
	if !removed["claude-code"] || !removed["antigravity"] {
		t.Fatalf("reset rows:\n%s", stdout)
	}
	if got, _ := os.ReadFile(antigravityFilePath(home)); string(got) != antigravityUserFile {
		t.Fatalf("reset did not leave the siblings as they were:\n%s", got)
	}
	if _, err := os.Stat(antigravityScriptDir(home)); !os.IsNotExist(err) {
		t.Fatalf("antigravity script copy remains: %v", err)
	}
}

// TestHooksPack_AntigravityExamplePack imports the shipped example hook,
// projects it to Antigravity, and runs the projected command the way a
// shell would, with Antigravity's camelCase payload on stdin. The test runs
// the script; gridctl never does.
func TestHooksPack_AntigravityExamplePack(t *testing.T) {
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
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	rows, code := hookSyncJSON(t, bin, home, "--clients", "antigravity", "block-force-push")
	if code != 0 || len(rows) != 1 || rows[0].Action != "copied" {
		t.Fatalf("sync exit %d rows %+v", code, rows)
	}
	v, ok := antigravityKeyValue(t, home)
	if !ok || len(v.PreToolUse) != 1 || v.PreToolUse[0].Matcher != "run_command" {
		t.Fatalf("key = %+v", v)
	}
	command := v.PreToolUse[0].Hooks[0].Command
	script := filepath.Join(antigravityScriptDir(home), "scripts", "block-force-push.sh")
	if command != "'"+script+"'" {
		t.Fatalf("command = %q", command)
	}
	if copied, err := os.ReadFile(script); err != nil || !bytes.Equal(copied, example) {
		t.Fatalf("%s does not hold the pinned example bytes: %v", script, err)
	}

	const reason = "git push --force is blocked by the network-eng pack"
	run := func(commandLine string) (int, string, string) {
		payload, err := json.Marshal(map[string]any{
			"conversationId": "c",
			"stepIdx":        1,
			"workspacePaths": []string{home},
			"toolCall": map[string]any{
				"name": "run_command",
				"args": map[string]string{"CommandLine": commandLine, "Cwd": home},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
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
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(stdout), &deny); err != nil || deny.Decision != "deny" || deny.Reason != reason {
		t.Fatalf("deny stdout %q: %v", stdout, err)
	}
	status, stdout, stderr = run("git push origin main")
	if status != 0 || stdout != "" || stderr != "" {
		t.Fatalf("allow exit %d stdout %q stderr %q", status, stdout, stderr)
	}
}
