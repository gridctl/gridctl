package scenarioverify

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVerifiedTests_PreservesGoFailure(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "run-verified-tests.sh")
	dir := t.TempDir()
	goTest := filepath.Join(dir, "fake-go-test")
	verifier := filepath.Join(dir, "fake-verifier")
	index := filepath.Join(dir, "index.yaml")
	if err := os.WriteFile(index, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goTest, []byte("#!/bin/sh\necho '{\"Action\":\"pass\",\"Package\":\"example.com/x\"}'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(verifier, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", script, "--lane", "unit", "--index", index, "--", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GO_TEST_CMD="+goTest,
		"VERIFIER_CMD="+verifier,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure, output %s", out)
	}
	if cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("exit = %d output=%s", cmd.ProcessState.ExitCode(), out)
	}
	if !strings.Contains(string(out), "go_status=1") {
		t.Fatalf("missing go status accounting: %s", out)
	}
}

func TestRunVerifiedTests_PreservesVerifierFailure(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "run-verified-tests.sh")
	dir := t.TempDir()
	goTest := filepath.Join(dir, "fake-go-test")
	verifier := filepath.Join(dir, "fake-verifier")
	index := filepath.Join(dir, "index.yaml")
	if err := os.WriteFile(index, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goTest, []byte("#!/bin/sh\necho '{\"Action\":\"pass\",\"Package\":\"example.com/x\"}'\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(verifier, []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", script, "--lane", "unit", "--index", index, "--", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GO_TEST_CMD="+goTest,
		"VERIFIER_CMD="+verifier,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected verifier failure, output %s", out)
	}
	if cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("exit = %d output=%s", cmd.ProcessState.ExitCode(), out)
	}
	if !strings.Contains(string(out), "verifier_status=1") {
		t.Fatalf("missing verifier status accounting: %s", out)
	}
}

func TestRunVerifiedTests_InjectsJSONCountRace(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "run-verified-tests.sh")
	dir := t.TempDir()
	goTest := filepath.Join(dir, "fake-go-test")
	verifier := filepath.Join(dir, "fake-verifier")
	index := filepath.Join(dir, "index.yaml")
	argsLog := filepath.Join(dir, "args")
	if err := os.WriteFile(index, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goTest, []byte("#!/bin/sh\nprintf '%s\n' \"$@\" >"+argsLog+"\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(verifier, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", script, "--lane", "unit", "--index", index, "--", "-coverprofile=coverage.out", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GO_TEST_CMD="+goTest,
		"VERIFIER_CMD="+verifier,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unexpected failure: %v %s", err, out)
	}
	args, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	got := string(args)
	if !strings.Contains(got, "-json") || !strings.Contains(got, "-count=1") || !strings.Contains(got, "-race") {
		t.Fatalf("missing required flags: %s", got)
	}
	if !strings.Contains(got, "-coverprofile=coverage.out") {
		t.Fatalf("coverage flag dropped: %s", got)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
