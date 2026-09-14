package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestValidateCheckMutableRefsDefaultOff(t *testing.T) {
	flag := validateCmd.Flags().Lookup("check-mutable-refs")
	if flag == nil {
		t.Fatal("missing --check-mutable-refs")
	}
	if flag.DefValue != "false" {
		t.Fatalf("default = %q, want false", flag.DefValue)
	}
}

func TestPrintValidationResult_DefaultSuppressesInfoOnly(t *testing.T) {
	var buf bytes.Buffer
	result := &config.ValidationResult{
		Valid: true,
		Issues: []config.ValidationIssue{{
			Field:    "mcp-servers[0].command",
			Message:  "reference-not-assessed: selector contains a variable; it was not classified.",
			Severity: config.SeverityInfo,
		}},
	}
	printValidationResultMode(&buf, "stack.yaml", result, false)
	out := buf.String()
	if out != "✓ stack.yaml is valid\n" {
		t.Fatalf("default rendering changed: %q", out)
	}
}

func TestPrintValidationResult_OptInShowsInfoAndCoverage(t *testing.T) {
	var buf bytes.Buffer
	result := &config.ValidationResult{
		Valid: true,
		Issues: []config.ValidationIssue{{
			Field:    "mcp-servers[0].image",
			Message:  "reference-not-assessed: selector contains a variable; it was not classified.",
			Severity: config.SeverityInfo,
		}},
	}
	printValidationResultMode(&buf, "stack.yaml", result, true)
	out := buf.String()
	if !strings.Contains(out, "✓ stack.yaml is valid") {
		t.Fatalf("missing valid header: %q", out)
	}
	if !strings.Contains(out, "mcp-servers[0].image") {
		t.Fatalf("missing info finding: %q", out)
	}
	if !strings.Contains(out, "reference-coverage:") {
		t.Fatalf("missing coverage: %q", out)
	}
	if strings.Contains(out, "s3cret") {
		t.Fatalf("leaked secret: %q", out)
	}
}

func TestPrintValidationResult_OptInNOCOLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	result := &config.ValidationResult{Valid: true}
	printValidationResultMode(&buf, "stack.yaml", result, true)
	out := buf.String()
	if strings.Contains(out, "✓") || strings.Contains(out, "ℹ") {
		t.Fatalf("NO_COLOR still used symbols: %q", out)
	}
	if !strings.Contains(out, "OK stack.yaml is valid") {
		t.Fatalf("missing textual severity: %q", out)
	}
	if !strings.Contains(out, "info coverage:") {
		t.Fatalf("missing textual info: %q", out)
	}
}

func TestValidateMutableRefsCLI(t *testing.T) {
	bin := buildValidateBinary(t)
	mutable := writeValidateStack(t, `name: mutable
mcp-servers:
  - name: s
    image: alpine:latest
    port: 3000
`)
	pinnedInfo := writeValidateStack(t, `name: info
mcp-servers:
  - name: s
    command: ["sh", "-c", "sleep 1"]
`)
	partial := writeValidateStack(t, `name: partial
mcp-servers:
  - name: s
    command: ["npx", "-y", "@scope/pkg@1.2.x"]
`)
	localPath := writeValidateStack(t, `name: local
mcp-servers:
  - name: s
    command: ["npx", "./tool"]
`)
	errorsAndWarnings := writeValidateStack(t, `name: errs
mcp-servers:
  - name: s
    image: alpine:latest
    port: 3000
  - name: s
    image: alpine:latest
    port: 3001
`)

	t.Run("default omits advisory findings", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, mutable)
		if code != 0 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		if strings.Contains(out, "mutable-image-reference") {
			t.Fatalf("default output included advisory finding: %s", out)
		}
	})
	t.Run("opt-in warning exit two", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, mutable, "--check-mutable-refs")
		if code != 2 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		if !strings.Contains(out, "mutable-image-reference:") {
			t.Fatalf("missing warning: %s", out)
		}
	})
	t.Run("info-only exit zero", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, pinnedInfo, "--check-mutable-refs")
		if code != 0 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		if !strings.Contains(out, "reference-not-assessed:") {
			t.Fatalf("missing info finding: %s", out)
		}
		if !strings.Contains(out, "command wrapper is unsupported") {
			t.Fatalf("missing wrapper coverage: %s", out)
		}
	})
	t.Run("shell wrapper json", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, pinnedInfo, "--check-mutable-refs", "--json")
		if code != 0 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		var result config.ValidationResult
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		found := false
		for _, issue := range result.Issues {
			if issue.Field == "mcp-servers[0].command" && strings.Contains(issue.Message, "command wrapper is unsupported") && issue.Severity == config.SeverityInfo {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing wrapper info finding: %s", out)
		}
	})
	t.Run("errors plus warnings exit one", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, errorsAndWarnings, "--check-mutable-refs")
		if code != 1 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		if !strings.Contains(out, "mutable-image-reference:") {
			t.Fatalf("missing warning with errors: %s", out)
		}
	})
	t.Run("partial npm json format flag", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, partial, "--check-mutable-refs", "--format", "json")
		if code != 2 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		assertMutablePackageJSON(t, out, "mcp-servers[0].command")
	})
	t.Run("local path json", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, localPath, "--check-mutable-refs", "--json")
		if code != 0 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		var result config.ValidationResult
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		found := false
		for _, issue := range result.Issues {
			if issue.Field == "mcp-servers[0].command" && strings.Contains(issue.Message, "local paths are not classified") && issue.Severity == config.SeverityInfo {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing local-path info finding: %s", out)
		}
	})
	t.Run("partial npm json alias flag", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, partial, "--check-mutable-refs", "--json")
		if code != 2 {
			t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr, out)
		}
		assertMutablePackageJSON(t, out, "mcp-servers[0].command")
	})
}

func buildValidateBinary(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "gridctl")
	build := exec.Command("go", "build", "-o", binPath, ".")
	out, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("building gridctl: %v\n%s", err, out)
	}
	return binPath
}

func writeValidateStack(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func runValidateBin(t *testing.T, bin, stack string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmdArgs := append([]string{"validate"}, args...)
	cmdArgs = append(cmdArgs, stack)
	cmd := exec.Command(bin, cmdArgs...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run validate: %v", err)
		}
	}
	return outBuf.String(), errBuf.String(), code
}

func assertMutablePackageJSON(t *testing.T, raw, field string) {
	t.Helper()
	var result config.ValidationResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, raw)
	}
	for _, issue := range result.Issues {
		if issue.Field == field && strings.HasPrefix(issue.Message, "mutable-package-reference:") && issue.Severity == config.SeverityWarning {
			return
		}
	}
	t.Fatalf("missing mutable package finding for %s in %s", field, raw)
}
