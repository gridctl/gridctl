package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVarExport_DeniedOnlyJSONRemainsValid(t *testing.T) {
	writeLegacyVarStore(t, `{"version":2,"variables":[{"key":"GRIDCTL_VAULT_PASSPHRASE","value":"denied-value","type":"string","is_secret":true}]}`)
	previousFormat, previousPlain := varExportFmt, varExportPlain
	varExportFmt, varExportPlain = "json", true
	t.Cleanup(func() { varExportFmt, varExportPlain = previousFormat, previousPlain })

	var runErr error
	out := captureStdout(t, func() { runErr = runVarExport() })
	if runErr != nil {
		t.Fatal(runErr)
	}
	var result struct {
		Variables []any    `json:"variables"`
		Warnings  []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not JSON: %v; output=%q", err, out)
	}
	if len(result.Variables) != 0 || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "GRIDCTL_VAULT_PASSPHRASE") {
		t.Fatalf("result = %+v", result)
	}
	if strings.Contains(out, "denied-value") {
		t.Fatal("JSON export leaked denied value")
	}
}

func TestRunVarExport_EnvWarningsUseStderr(t *testing.T) {
	writeLegacyVarStore(t, `{"version":2,"variables":[{"key":"SAFE","value":"safe-value","type":"string","is_secret":false},{"key":"OP_CONNECT_TOKEN","value":"denied-value","type":"string","is_secret":true}]}`)
	previousFormat, previousPlain := varExportFmt, varExportPlain
	varExportFmt, varExportPlain = "env", true
	t.Cleanup(func() { varExportFmt, varExportPlain = previousFormat, previousPlain })

	var runErr error
	stderr := captureStderr(t, func() {
		stdout := captureStdout(t, func() { runErr = runVarExport() })
		if stdout != "# @public\nSAFE=safe-value\n" {
			t.Fatalf("stdout = %q, want clean env content", stdout)
		}
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(stderr, "OP_CONNECT_TOKEN") || strings.Contains(stderr, "denied-value") {
		t.Fatalf("stderr warning = %q", stderr)
	}
}

func writeLegacyVarStore(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	dir := filepath.Join(home, ".gridctl", "vault")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secrets.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = original })
	done := make(chan string, 1)
	go func() {
		var buffer bytes.Buffer
		_, _ = buffer.ReadFrom(r)
		done <- buffer.String()
	}()
	fn()
	_ = w.Close()
	result := <-done
	os.Stderr = original
	return result
}
