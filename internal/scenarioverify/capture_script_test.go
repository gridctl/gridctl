package scenarioverify

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runVerifiedScript(t *testing.T, env []string, args ...string) ([]byte, *exec.Cmd) {
	t.Helper()
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "run-verified-tests.sh")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "bash", append([]string{script}, args...)...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), env...)
	out, _ := cmd.CombinedOutput()
	return out, cmd
}

func TestRunVerifiedTests_PreservesGoFailure(t *testing.T) {
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

	out, cmd := runVerifiedScript(t, []string{"GO_TEST_CMD=" + goTest, "VERIFIER_CMD=" + verifier},
		"--lane", "unit", "--index", index, "--", "./...")
	if cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("exit = %d output=%s", cmd.ProcessState.ExitCode(), out)
	}
	if !strings.Contains(string(out), "go_status=1") {
		t.Fatalf("missing go status accounting: %s", out)
	}
}

func TestRunVerifiedTests_PreservesVerifierFailure(t *testing.T) {
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

	out, cmd := runVerifiedScript(t, []string{"GO_TEST_CMD=" + goTest, "VERIFIER_CMD=" + verifier},
		"--lane", "unit", "--index", index, "--", "./...")
	if cmd.ProcessState.ExitCode() != 1 {
		t.Fatalf("exit = %d output=%s", cmd.ProcessState.ExitCode(), out)
	}
	if !strings.Contains(string(out), "verifier_status=1") {
		t.Fatalf("missing verifier status accounting: %s", out)
	}
}

func TestRunVerifiedTests_InjectsJSONCountRace(t *testing.T) {
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

	out, cmd := runVerifiedScript(t, []string{"GO_TEST_CMD=" + goTest, "VERIFIER_CMD=" + verifier},
		"--lane", "unit", "--index", index, "--", "-coverprofile=coverage.out", "./...")
	if cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("unexpected failure: exit=%d %s", cmd.ProcessState.ExitCode(), out)
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

func passingCaptureIndex(t *testing.T) (index, goTest string) {
	t.Helper()
	dir := t.TempDir()
	index = filepath.Join(dir, "index.yaml")
	goTest = filepath.Join(dir, "fake-go-test")
	body := []byte(`
version: 1
lanes: [unit]
scenarios:
  - id: required-child
    owner_package: pkg/mcp
    owner_issue: "1227"
    package: example.com/mcp
    test: TestFoo
    lanes: [unit]
    expected_boundary: denied before dispatch
`)
	if err := os.WriteFile(index, body, 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n" +
		"echo '{\"Action\":\"run\",\"Package\":\"example.com/mcp\",\"Test\":\"TestFoo\"}'\n" +
		"echo '{\"Action\":\"pass\",\"Package\":\"example.com/mcp\",\"Test\":\"TestFoo\"}'\n" +
		"echo '{\"Action\":\"pass\",\"Package\":\"example.com/mcp\"}'\n" +
		"exit 0\n"
	if err := os.WriteFile(goTest, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return index, goTest
}

func TestRunVerifiedTests_PreservesCaptureFailure(t *testing.T) {
	index, goTest := passingCaptureIndex(t)
	captureDir := t.TempDir()
	out, cmd := runVerifiedScript(t, []string{"GO_TEST_CMD=" + goTest},
		"--lane", "unit", "--index", index, "--capture", captureDir, "--", "./...")
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() == 0 {
		t.Fatalf("expected capture failure, output %s", out)
	}
	text := string(out)
	if !strings.Contains(text, "capture_status=") || strings.Contains(text, "capture_status=0") {
		t.Fatalf("missing nonzero capture status: %s", text)
	}
	if !strings.Contains(text, "Required scenarios:") && !strings.Contains(text, "verifier_status=") {
		t.Fatalf("diagnostic verification did not run: %s", text)
	}
}

func TestRunVerifiedTests_RealVerifierPass(t *testing.T) {
	index, goTest := passingCaptureIndex(t)
	out, cmd := runVerifiedScript(t, []string{"GO_TEST_CMD=" + goTest},
		"--lane", "unit", "--index", index, "--", "./...")
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("real verifier failed: exit=%v output=%s", cmd.ProcessState, out)
	}
	if !strings.Contains(string(out), "Required scenarios: PASS") {
		t.Fatalf("missing pass summary: %s", out)
	}
}

func TestRunVerifiedTests_SummaryPathKeepsSuccess(t *testing.T) {
	index, goTest := passingCaptureIndex(t)
	summary := filepath.Join(t.TempDir(), "unit-scenarios.json")
	out, cmd := runVerifiedScript(t, []string{"GO_TEST_CMD=" + goTest},
		"--lane", "unit", "--index", index, "--summary", summary, "--", "-coverprofile=coverage.out", "./...")
	if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 0 {
		t.Fatalf("caller-supplied summary changed exit: exit=%v output=%s", cmd.ProcessState, out)
	}
	if !strings.Contains(string(out), "Required scenarios: PASS") {
		t.Fatalf("missing pass summary: %s", out)
	}
	if _, err := os.Stat(summary); err != nil {
		t.Fatalf("summary was not written: %v", err)
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
