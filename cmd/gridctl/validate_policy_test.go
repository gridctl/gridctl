package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/stackpolicy"
)

func TestValidatePolicyFlagDefaultOff(t *testing.T) {
	flag := validateCmd.Flags().Lookup("policy")
	if flag == nil {
		t.Fatal("missing --policy")
	}
	if flag.DefValue != "" {
		t.Fatalf("default = %q", flag.DefValue)
	}
}

func TestValidatePolicyCLI(t *testing.T) {
	bin := buildValidateBinary(t)
	dir := t.TempDir()
	pinned := "alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce"
	stack := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(stack, []byte("name: demo\nmcp-servers:\n  - name: s\n    image: "+pinned+"\n    port: 1\n    tools: [\"read\", \"*\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	policy := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(policy, []byte("version: \"1\"\nenabled:\n  - explicit-image-digests\n  - nonempty-server-tool-lists\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mutable := filepath.Join(dir, "mutable.yaml")
	if err := os.WriteFile(mutable, []byte("name: demo\nmcp-servers:\n  - name: s\n    image: alpine:latest\n    port: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tagPolicy := filepath.Join(dir, "tags.yaml")
	if err := os.WriteFile(tagPolicy, []byte("version: \"1\"\nenabled:\n  - explicit-image-digests\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("without policy remains ordinary validate", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, mutable)
		if code != 0 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		if strings.Contains(out, "Declared-stack policy") {
			t.Fatalf("policy path taken: %s", out)
		}
	})
	t.Run("accepted with wildcard warning exit two", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, stack, "--policy", policy)
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		if !strings.Contains(out, "Declared-stack policy accepted") {
			t.Fatalf("stdout=%s", out)
		}
		if !strings.Contains(out, stackpolicy.WarningLiteralToolNameNotPattern) {
			t.Fatalf("missing warning: %s", out)
		}
		if strings.Contains(out, `"*"`) {
			t.Fatalf("echoed tool: %s", out)
		}
	})
	t.Run("json document", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, stack, "--policy", policy, "--json")
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		var report stackpolicy.Report
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		if !report.Accepted || report.SchemaVersion != stackpolicy.SchemaVersion {
			t.Fatalf("%+v", report)
		}
		if strings.Contains(out, "progress") {
			t.Fatal(out)
		}
	})
	t.Run("violation exit one", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, mutable, "--policy", tagPolicy)
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		if !strings.Contains(out, "VIOLATION") {
			t.Fatalf("stdout=%s", out)
		}
	})
	t.Run("missing policy does not fall back", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, mutable, "--policy", filepath.Join(dir, "nope.yaml"))
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		if strings.Contains(out, "is valid") {
			t.Fatalf("fell back: %s", out)
		}
	})
	t.Run("empty policy flag", func(t *testing.T) {
		out, stderr, code := runValidateBin(t, bin, mutable, "--policy", "")
		if code != 1 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		if strings.Contains(out, "is valid") {
			t.Fatalf("fell back: %s", out)
		}
	})
	t.Run("policy json without home", func(t *testing.T) {
		out, stderr, code := runValidateBinEnv(t, bin, withoutHomeEnv(t), stack, "--policy", policy, "--json")
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		var report stackpolicy.Report
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		if !report.Accepted {
			t.Fatalf("%+v", report)
		}
		if strings.Count(out, `"schema_version"`) != 1 {
			t.Fatalf("want one json document: %s", out)
		}
		if strings.Contains(stderr, "HOME") || strings.Contains(out, "HOME") {
			t.Fatalf("home leaked into policy path: stdout=%s stderr=%s", out, stderr)
		}
	})
	t.Run("policy json with invalid home override", func(t *testing.T) {
		env := append(withoutHomeEnv(t), "GRIDCTL_HOME=not-absolute")
		out, stderr, code := runValidateBinEnv(t, bin, env, stack, "--policy", policy, "--json")
		if code != 2 {
			t.Fatalf("exit=%d stderr=%s stdout=%s", code, stderr, out)
		}
		var report stackpolicy.Report
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("json: %v\n%s", err, out)
		}
		if !report.Accepted {
			t.Fatalf("%+v", report)
		}
	})
	t.Run("ordinary validate still requires home", func(t *testing.T) {
		out, stderr, code := runValidateBinEnv(t, bin, withoutHomeEnv(t), mutable)
		if code == 0 {
			t.Fatalf("ordinary validate succeeded without home: stdout=%s stderr=%s", out, stderr)
		}
		if strings.Contains(out, "Declared-stack policy") {
			t.Fatalf("policy path taken: %s", out)
		}
	})
}
