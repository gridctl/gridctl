package varrun

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/vault"
)

func TestRun_EnvironmentRedactionAndExit(t *testing.T) {
	if os.Getenv("VAR_RUN_HELPER") == "1" {
		_, _ = os.Stdout.WriteString(os.Getenv("TOKEN"))
		_, _ = os.Stderr.WriteString("err:" + os.Getenv("LIST"))
		os.Exit(3)
	}
	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), Options{
		Command:     []string{os.Args[0], "-test.run=TestRun_EnvironmentRedactionAndExit"},
		Environment: append(os.Environ(), "VAR_RUN_HELPER=1", "TOKEN=ambient"),
		Variables:   []vault.Variable{{Key: "TOKEN", Value: "selected-secret", IsSecret: true}, {Key: "LIST", Value: `["a","b"]`, Type: vault.TypeList}},
		Stdout:      &stdout, Stderr: &stderr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 3 {
		t.Fatalf("exit = %d", result.ExitCode)
	}
	if stdout.String() != "[REDACTED]" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), `["a","b"]`) {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRun_StdinPlaintextAndCredentialStripping(t *testing.T) {
	if os.Getenv("VAR_RUN_IO_HELPER") == "1" {
		input, _ := io.ReadAll(os.Stdin)
		_, _ = os.Stdout.WriteString(string(input) + ":" + os.Getenv("TOKEN") + ":" + os.Getenv("GRIDCTL_VAULT_PASSPHRASE"))
		_, _ = os.Stderr.WriteString("separate")
		os.Exit(0)
	}
	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), Options{
		Command: []string{os.Args[0], "-test.run=TestRun_StdinPlaintextAndCredentialStripping"},
		Environment: []string{
			"VAR_RUN_IO_HELPER=1",
			"GOCOVERDIR=" + t.TempDir(),
			"TOKEN=ambient",
			"GRIDCTL_VAULT_PASSPHRASE=ambient-passphrase",
		},
		Variables: []vault.Variable{
			{Key: "TOKEN", Value: "stored", IsSecret: false},
			{Key: "GRIDCTL_VAULT_PASSPHRASE", Value: "selected-passphrase", IsSecret: true},
		},
		Stdin: strings.NewReader("input"), Stdout: &stdout, Stderr: &stderr, NoRedact: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || stdout.String() != "input:stored:" || stderr.String() != "separate" {
		t.Fatalf("result=%+v stdout=%q stderr=%q", result, stdout.String(), stderr.String())
	}
}
