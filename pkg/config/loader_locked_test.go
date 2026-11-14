package config

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
)

// lockedVault is a store double that reports itself locked and resolves nothing.
type lockedVault struct{}

func (lockedVault) Get(string) (string, bool) { return "", false }
func (lockedVault) IsLocked() bool            { return true }

// plainSetVault satisfies VaultSetLookup without reporting a locked state.
type plainSetVault struct{}

func (plainSetVault) Get(string) (string, bool)          { return "", false }
func (plainSetVault) GetSetSecrets(string) []VaultSecret { return nil }

// lockedSetVault satisfies VaultSetLookup and reports itself locked.
type lockedSetVault struct{ plainSetVault }

func (lockedSetVault) IsLocked() bool { return true }

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}

const lockedStackYAML = `
name: locked-test
network:
  name: test-net
mcp-servers:
  - name: remote
    url: https://mcp.example.com/mcp
    headers:
      Authorization: "Bearer ${var:DEMO_TOKEN}"
`

func TestLoadStack_LockedStore_ReportsLockedNotMissing(t *testing.T) {
	unsetEnv(t, "DEMO_TOKEN")
	path := writeTempFile(t, lockedStackYAML)

	_, err := LoadStack(path, WithVault(lockedVault{}))
	if err == nil {
		t.Fatal("expected a locked-store error")
	}
	if !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("errors.Is(err, ErrVaultLocked) = false: %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"locked", "DEMO_TOKEN", "GRIDCTL_VAULT_PASSPHRASE", "GRIDCTL_VAULT_PASSPHRASE_FILE", "gridctl var explain <KEY>"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	for _, reject := range []string{"missing variable(s)", "var set"} {
		if strings.Contains(msg, reject) {
			t.Errorf("error contains %q:\n%s", reject, msg)
		}
	}
}

func TestLoadStack_LockedStore_UsesSetLookupWhenVaultCannotReport(t *testing.T) {
	unsetEnv(t, "DEMO_TOKEN")
	path := writeTempFile(t, lockedStackYAML)

	_, err := LoadStack(path, WithVault(&mockVault{secrets: map[string]string{}}), WithVaultSets(lockedSetVault{}))
	if !errors.Is(err, ErrVaultLocked) {
		t.Fatalf("err = %v, want ErrVaultLocked", err)
	}
}

func TestLoadStack_UnlockedStore_MissingKeyTextUnchanged(t *testing.T) {
	unsetEnv(t, "DEMO_TOKEN")
	path := writeTempFile(t, lockedStackYAML)

	_, err := LoadStack(path, WithVault(&mockVault{secrets: map[string]string{}}), WithVaultSets(plainSetVault{}))
	if err == nil {
		t.Fatal("expected a missing-variable error")
	}
	if errors.Is(err, ErrVaultLocked) {
		t.Fatalf("unlocked store reported locked: %v", err)
	}
	want := "missing variable(s): DEMO_TOKEN\n  To fix: gridctl var set <KEY>\n  Diagnose: gridctl var explain <KEY>"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestLoadStack_LockedStore_EnvFallbackWarns(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []LoadOption
	}{
		{name: "default", opts: []LoadOption{WithVault(lockedVault{})}},
		{name: "quiet", opts: []LoadOption{WithVault(lockedVault{}), WithQuietLoad()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEMO_TOKEN", "from-env")
			logs := captureSlog(t)
			path := writeTempFile(t, lockedStackYAML)

			stack, err := LoadStack(path, tc.opts...)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if got := stack.MCPServers[0].Headers["Authorization"]; got != "Bearer from-env" {
				t.Fatalf("Authorization = %q", got)
			}
			out := logs.String()
			if n := strings.Count(out, "level=WARN"); n != 1 {
				t.Fatalf("got %d warnings, want 1:\n%s", n, out)
			}
			if !strings.Contains(out, "DEMO_TOKEN") || !strings.Contains(out, "variable store is locked") {
				t.Fatalf("warning does not name the key and the locked store:\n%s", out)
			}
			if strings.Contains(out, "from-env") {
				t.Fatalf("warning leaked the value:\n%s", out)
			}
		})
	}
}

func TestLoadStack_UnlockedStore_EnvFallbackDoesNotWarn(t *testing.T) {
	t.Setenv("DEMO_TOKEN", "from-env")
	logs := captureSlog(t)
	path := writeTempFile(t, lockedStackYAML)

	if _, err := LoadStack(path, WithVault(&mockVault{secrets: map[string]string{}})); err != nil {
		t.Fatalf("load: %v", err)
	}
	if strings.Contains(logs.String(), "locked") {
		t.Fatalf("unlocked store produced a locked warning:\n%s", logs.String())
	}
}

func TestLoadStack_LockedStore_SecretSetsWarns(t *testing.T) {
	const content = `
name: locked-sets
network:
  name: test-net
secrets:
  sets:
    - dev
mcp-servers:
  - name: remote
    url: https://mcp.example.com/mcp
`
	for _, tc := range []struct {
		name string
		opts []LoadOption
	}{
		{name: "vault decides first", opts: []LoadOption{WithVault(lockedVault{}), WithVaultSets(plainSetVault{})}},
		{name: "set lookup fallback", opts: []LoadOption{WithVaultSets(lockedSetVault{})}},
		{name: "quiet", opts: []LoadOption{WithVault(lockedVault{}), WithVaultSets(plainSetVault{}), WithQuietLoad()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureSlog(t)
			path := writeTempFile(t, content)
			if _, err := LoadStack(path, tc.opts...); err != nil {
				t.Fatalf("load: %v", err)
			}
			out := logs.String()
			if strings.Count(out, "secrets.sets injected nothing because the variable store is locked") != 1 {
				t.Fatalf("want one secrets.sets warning:\n%s", out)
			}
		})
	}

	t.Run("unlocked does not warn", func(t *testing.T) {
		logs := captureSlog(t)
		path := writeTempFile(t, content)
		if _, err := LoadStack(path, WithVault(&mockVault{secrets: map[string]string{}}), WithVaultSets(plainSetVault{})); err != nil {
			t.Fatalf("load: %v", err)
		}
		if strings.Contains(logs.String(), "secrets.sets") {
			t.Fatalf("unlocked store warned:\n%s", logs.String())
		}
	})
}
