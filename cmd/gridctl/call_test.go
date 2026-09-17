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
	t.Run("omitted", func(t *testing.T) {
		got, err := parseCallArguments("")
		if err != nil || len(got) != 0 {
			t.Fatalf("got %#v err=%v", got, err)
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
