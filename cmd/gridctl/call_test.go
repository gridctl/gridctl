package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCallArguments(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		if _, err := parseCallArguments(""); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("whitespace", func(t *testing.T) {
		if _, err := parseCallArguments("   "); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(path, []byte(" \n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := parseCallArguments("@" + path); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("number precision", func(t *testing.T) {
		got, err := parseCallArguments(`{"n":1.2300}`)
		if err != nil {
			t.Fatal(err)
		}
		n, ok := got["n"].(json.Number)
		if !ok || n.String() != "1.2300" {
			t.Fatalf("got %#v", got["n"])
		}
	})
	t.Run("rejects array", func(t *testing.T) {
		if _, err := parseCallArguments(`[]`); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("rejects trailing", func(t *testing.T) {
		if _, err := parseCallArguments(`{"a":1}{"b":2}`); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "args.json")
		if err := os.WriteFile(path, []byte(`{"message":"hi"}`), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := parseCallArguments("@" + path)
		if err != nil || got["message"] != "hi" {
			t.Fatalf("got %#v err=%v", got, err)
		}
	})
	t.Run("unreadable", func(t *testing.T) {
		if _, err := parseCallArguments("@/no/such/file.json"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("oversized file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "big.json")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString(`{"x":"`)
		buf := bytes.Repeat([]byte("a"), callArgsMaxBytes)
		_, _ = f.Write(buf)
		_, _ = f.WriteString(`"}`)
		_ = f.Close()
		_, err = parseCallArguments("@" + path)
		if !errors.Is(err, errArgsTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestParseCallTarget(t *testing.T) {
	if _, err := parseCallTarget("echo"); err == nil {
		t.Fatal("bare server")
	}
	if _, err := parseCallTarget("echo__"); err == nil {
		t.Fatal("empty tool")
	}
	if _, err := parseCallTarget("__echo"); err == nil {
		t.Fatal("empty server")
	}
	if got, err := parseCallTarget("echo__echo"); err != nil || got != "echo__echo" {
		t.Fatalf("got %s %v", got, err)
	}
}

func TestExitStatusTypedError(t *testing.T) {
	if exitStatus(nil) != 0 {
		t.Fatal("nil")
	}
	if exitStatus(errors.New("boom")) != 1 {
		t.Fatal("plain")
	}
	wrapped := errors.Join(errors.New("outer"), &commandError{exit2: true, human: "tool error"})
	if exitStatus(wrapped) != 2 {
		t.Fatal("wrapped tool error")
	}
	if exitStatus(&commandError{human: "denied"}) != 1 {
		t.Fatal("non-tool command error")
	}
}

func TestRenderCommandErrorJSONOnce(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := newCommandError(true, localFailureEnvelope("cli", "echo__echo", "input", reasonInvalidJSON, "invalid JSON"), "invalid JSON", false)
	if !renderCommandError(stdout, stderr, callCmd, err) {
		t.Fatal("expected render")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %s", stderr.String())
	}
	var env cliCallEnvelope
	if json.Unmarshal(stdout.Bytes(), &env) != nil || env.Error == nil || env.Error.Code != reasonInvalidJSON {
		t.Fatalf("stdout = %s", stdout.String())
	}
	if strings.Count(stdout.String(), `"schema_version"`) != 1 {
		t.Fatal("expected one JSON document")
	}
}

func TestUnrelatedCommandErrorNotJSON(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if renderCommandError(stdout, stderr, validateCmd, errors.New("reading stack file")) {
		t.Fatal("unrelated command should use existing rendering")
	}
}

func TestExecuteC_OfflineCallHelp(t *testing.T) {
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs([]string{"call", "--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	_, err := executeC()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(outBuf.String(), "Invoke one canonical tool") {
		t.Fatalf("help = %s", outBuf.String())
	}
}

func TestExecuteC_InvalidTargetNoDaemon(t *testing.T) {
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs([]string{"call", "echo", "--format", "json"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	_, err := executeC()
	if exitStatus(err) != 1 {
		t.Fatalf("exit mapping: %v", err)
	}
	var ce *commandError
	if !errors.As(err, &ce) || ce.exit2 {
		t.Fatalf("err = %v", err)
	}
}

func TestArgsWantJSON(t *testing.T) {
	if !argsWantJSON([]string{"call", "echo__echo", "--bad", "--json"}) {
		t.Fatal("json after unknown flag")
	}
	if !argsWantJSON([]string{"call", "echo__echo", "--json", "--bad"}) {
		t.Fatal("json before unknown flag")
	}
	if !argsWantJSON([]string{"call", "--format", "json", "echo__echo"}) {
		t.Fatal("format json")
	}
	if argsWantJSON([]string{"call", "echo__echo", "--format", "invalid", `{"--json":true}`}) {
		t.Fatal("payload must not enable json")
	}
	if !argsWantJSON([]string{"call", "echo__echo", "--json", "--format", "invalid"}) {
		t.Fatal("json alias survives format conflict")
	}
}

func TestDeclaredCLIClient(t *testing.T) {
	got, err := declaredCLIClient("", false)
	if err != nil || got != "cli" {
		t.Fatalf("default = %q %v", got, err)
	}
	got, err = declaredCLIClient("cursor ide", true)
	if err != nil || got != "cursor ide" {
		t.Fatalf("raw label = %q %v", got, err)
	}
	if _, err := declaredCLIClient("  ", true); err == nil {
		t.Fatal("whitespace")
	}
}

func TestExecuteC_OfflineCallHelpWithFormatFlag(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs([]string{"call", "--format", "json", "--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	_, err := executeC()
	if err != nil {
		t.Fatalf("zero-target help must stay offline: %v stderr=%s", err, errBuf.String())
	}
	if !strings.Contains(outBuf.String(), "Invoke one canonical tool") {
		t.Fatalf("help = %s", outBuf.String())
	}
}

func TestExecuteC_HelpErrorClearedBetweenRuns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	outBuf, errBuf := &bytes.Buffer{}, &bytes.Buffer{}
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	rootCmd.SetArgs([]string{"call", "echo__echo", "--help"})
	if _, err := executeC(); err == nil {
		t.Fatal("expected targeted help failure without a daemon")
	}
	outBuf.Reset()
	errBuf.Reset()
	rootCmd.SetArgs([]string{"call", "--help"})
	if _, err := executeC(); err != nil {
		t.Fatalf("offline help after a failed live help: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Invoke one canonical tool") {
		t.Fatalf("help = %s", outBuf.String())
	}
}
