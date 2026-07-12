package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// executeCommand runs the root command with args and returns captured
// output. Cobra writes help to the configured out writer; errors are
// returned, not printed, because SilenceErrors is set globally.
func executeCommand(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	rootCmd.SetOut(outBuf)
	rootCmd.SetErr(errBuf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

func TestRootHelpGroupsAndQuickStart(t *testing.T) {
	out, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}

	for _, want := range []string{"QUICK START", "STACK", "CLIENTS", "SKILLS", "VARIABLES & PINS", "OBSERVABILITY", "SYSTEM", "gridctl apply stack.yaml", "EXAMPLES"} {
		if !strings.Contains(out, want) {
			t.Errorf("root help missing %q", want)
		}
	}
	if strings.Contains(out, "\033") {
		t.Error("root help contains ANSI escapes on a non-TTY writer")
	}
	if strings.Contains(out, "vault") {
		t.Error("deprecated vault command should stay hidden from root help")
	}
}

func TestLeafHelpHasExamplesAndNoFooter(t *testing.T) {
	out, _, err := executeCommand(t, "apply", "--help")
	if err != nil {
		t.Fatalf("apply --help: %v", err)
	}
	if !strings.Contains(out, "EXAMPLES") {
		t.Error("apply help missing EXAMPLES section")
	}
	if strings.Contains(out, `gridctl apply [command] --help`) {
		t.Error("leaf command help should not render the subcommand footer")
	}
}

func TestParentHelpKeepsFooter(t *testing.T) {
	out, _, err := executeCommand(t, "skill", "--help")
	if err != nil {
		t.Fatalf("skill --help: %v", err)
	}
	if !strings.Contains(out, `gridctl skill [command] --help`) {
		t.Error("parent command help should keep the subcommand footer")
	}
}

func TestRuntimeErrorPrintsNoUsage(t *testing.T) {
	out, errOut, err := executeCommand(t, "validate", "/no/such/file.yaml")
	if err == nil {
		t.Fatal("expected an error for a missing stack file")
	}
	combined := out + errOut
	if strings.Contains(combined, "Usage:") || strings.Contains(combined, "USAGE") {
		t.Errorf("runtime error should not dump usage, got %q", combined)
	}
	// SilenceErrors means cobra prints nothing; Execute owns the print.
	if strings.Contains(combined, err.Error()) {
		t.Errorf("error should not be printed by cobra itself, got %q", combined)
	}
}

func TestFlagErrorKeepsUsageGuidance(t *testing.T) {
	_, _, err := executeCommand(t, "status", "--not-a-flag")
	if err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("expected unknown flag error, got %q", err)
	}
	if !strings.Contains(err.Error(), "--help' for usage") {
		t.Errorf("flag error should point at --help, got %q", err)
	}
}

func TestUnknownCommandSuggests(t *testing.T) {
	_, _, err := executeCommand(t, "statuss")
	if err == nil {
		t.Fatal("expected an error for an unknown command")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected unknown command error, got %q", err)
	}
	if !strings.Contains(err.Error(), "status") || !strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("expected a suggestion for status, got %q", err)
	}
}

func TestPrintCLIErrorSingleLine(t *testing.T) {
	var buf bytes.Buffer
	printCLIError(&buf, nil, errors.New("boom"))
	out := buf.String()
	if out != "Error: boom\n" {
		t.Errorf("printCLIError = %q, want %q", out, "Error: boom\n")
	}
}

func TestPrintCLIErrorUnknownCommandPointer(t *testing.T) {
	var buf bytes.Buffer
	printCLIError(&buf, nil, errors.New(`unknown command "foo" for "gridctl"`))
	if !strings.Contains(buf.String(), "Run 'gridctl --help' for usage") {
		t.Errorf("expected help pointer for unknown command, got %q", buf.String())
	}
}

func TestPrintCLIErrorArgCountPointsAtCommandHelp(t *testing.T) {
	var buf bytes.Buffer
	printCLIError(&buf, destroyCmd, errors.New("accepts 1 arg(s), received 0"))
	if !strings.Contains(buf.String(), "Run 'gridctl destroy --help' for usage") {
		t.Errorf("expected a command-scoped help pointer, got %q", buf.String())
	}
}

func TestPrintCLIErrorSubcommandPointer(t *testing.T) {
	var buf bytes.Buffer
	printCLIError(&buf, skillCmd, errors.New(`unknown command "lst" for "gridctl skill"`))
	if !strings.Contains(buf.String(), "Run 'gridctl skill --help' for usage") {
		t.Errorf("expected the parent command's help path, got %q", buf.String())
	}
}

func TestPrintCLIErrorRuntimeErrorNoPointer(t *testing.T) {
	var buf bytes.Buffer
	printCLIError(&buf, destroyCmd, errors.New("reading stack file: no such file"))
	if strings.Contains(buf.String(), "--help") {
		t.Errorf("runtime errors must not carry a help pointer, got %q", buf.String())
	}
}
