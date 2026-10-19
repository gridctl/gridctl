package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/varscan"
	"github.com/spf13/cobra"
)

func TestSecretExplain_DeclaredResolvedUndeclaredAndNoResolve(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "provider.sh")
	body := "#!/bin/sh\nprintf 'resolved-secret-value\\n'\ntouch " + shellQuote(marker) + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	stackYAML := "version: \"1\"\nname: demo\nsecrets:\n  providers:\n    - name: fake\n      type: exec\n      command: [" + jsonString(script) + ", \"{{path}}\"]\nmcp-servers:\n  - name: echo\n    command: [\"true\"]\n    env:\n      TOKEN: ${secret:fake/alpha}\n"
	if err := os.WriteFile(stackPath, []byte(stackYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := explainCmd(t)
	exit, err := runSecretExplain(cmd, "secret:fake/alpha", "json", stackPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d\n%s", exit, cmd.OutOrStdout())
	}
	out := cmd.OutOrStdout().(*bytes.Buffer).String()
	if strings.Contains(out, "resolved-secret-value") {
		t.Fatal("explain printed the value")
	}
	var doc secretExplainDocument
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Reference != "secret:fake/alpha" || doc.Provider != "fake" || doc.Type != "exec" || doc.Path != "alpha" || !doc.Declared || doc.Resolved == nil || !*doc.Resolved || doc.Error != nil || len(doc.Consumers) != 1 {
		t.Fatalf("document = %+v", doc)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("provider did not run")
	}

	undeclaredPath := filepath.Join(dir, "undeclared.yaml")
	undeclared := strings.Replace(stackYAML, "secrets:\n  providers:\n    - name: fake\n      type: exec\n      command: ["+jsonString(script)+", \"{{path}}\"]\n", "", 1)
	if err := os.WriteFile(undeclaredPath, []byte(undeclared), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = explainCmd(t)
	exit, err = runSecretExplain(cmd, "secret:fake/alpha", "json", undeclaredPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if exit != 1 {
		t.Fatalf("undeclared exit = %d\n%s", exit, cmd.OutOrStdout())
	}
	doc = secretExplainDocument{}
	if err := json.Unmarshal(cmd.OutOrStdout().(*bytes.Buffer).Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Declared || doc.Resolved == nil || *doc.Resolved || len(doc.Consumers) != 1 {
		t.Fatalf("undeclared document = %+v", doc)
	}

	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	cmd = explainCmd(t)
	exit, err = runSecretExplain(cmd, "secret:fake/alpha", "json", stackPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if exit != 0 {
		t.Fatalf("--no-resolve exit = %d\n%s", exit, cmd.OutOrStdout())
	}
	raw := cmd.OutOrStdout().(*bytes.Buffer).Bytes()
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	if generic["resolved"] != nil {
		t.Fatalf("resolved = %#v, want null", generic["resolved"])
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("--no-resolve ran the provider")
	}
}

func TestSecretExplain_FailingProvider(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	body := "version: \"1\"\nname: demo\nsecrets:\n  providers:\n    - name: fake\n      type: exec\n      command: [" + jsonString(script) + ", \"{{path}}\"]\nmcp-servers:\n  - name: echo\n    command: [\"true\"]\n    env:\n      TOKEN: ${secret:fake/alpha}\n"
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := explainCmd(t)
	exit, err := runSecretExplain(cmd, "secret:fake/alpha", "json", stackPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if exit != 1 {
		t.Fatalf("exit = %d", exit)
	}
	var doc secretExplainDocument
	if err := json.Unmarshal(cmd.OutOrStdout().(*bytes.Buffer).Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Resolved == nil || *doc.Resolved || doc.Error == nil || !strings.Contains(*doc.Error, "provider exited 1") {
		t.Fatalf("document = %+v", doc)
	}
}

func TestSecretScanFile_StagedValueAndProviderSkip(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "provider.sh")
	fail := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'resolved-secret-value\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fail, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	body := "version: \"1\"\nname: demo\nsecrets:\n  providers:\n    - name: fake\n      type: exec\n      command: [" + jsonString(script) + ", \"{{path}}\"]\n    - name: bad\n      type: exec\n      command: [" + jsonString(fail) + ", \"{{path}}\"]\nmcp-servers:\n  - name: echo\n    command: [\"true\"]\n    env:\n      TOKEN: ${secret:fake/alpha}\n      OTHER: ${secret:bad/beta}\n"
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("safe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "notes.txt")
	gitCmd(t, repo, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("resolved-secret-value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "notes.txt")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("working tree is clean of the value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	previousFile, previousStaged := varScanFile, varScanStaged
	varScanFile, varScanStaged = stackPath, true
	t.Cleanup(func() { varScanFile, varScanStaged = previousFile, previousStaged })

	cmd := explainCmd(t)
	exit, err := runVarScan(cmd, nil, "json")
	if err != nil {
		t.Fatal(err)
	}
	if exit != 1 {
		t.Fatalf("exit = %d\n%s", exit, cmd.OutOrStdout())
	}
	var result varscan.Result
	if err := json.Unmarshal(cmd.OutOrStdout().(*bytes.Buffer).Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, finding := range result.Findings {
		if finding.Key == "secret:fake/alpha" {
			found = true
		}
		if strings.Contains(finding.Snippet, "resolved-secret-value") {
			t.Fatal("snippet contained the value")
		}
	}
	if !found {
		t.Fatalf("findings = %#v", result.Findings)
	}
	skipped := false
	for _, skip := range result.Skipped {
		if skip.Key == "secret:bad/beta" && strings.Contains(skip.Reason, "provider exited 1") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("skips = %#v", result.Skipped)
	}
}

func explainCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})
	return cmd
}

func jsonString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
