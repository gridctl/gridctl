package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVaultNeeds_VarReferenceDespiteEnvironment(t *testing.T) {
	t.Setenv("FROM_ENV", "present")
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	body := []byte("version: \"1\"\nname: needs-var\nmcp-servers:\n  - name: demo\n    command: [\"echo\"]\n    env:\n      TOKEN: \"${var:FROM_ENV}\"\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	got := VaultNeeds(stack)
	if len(got) != 1 || got[0] != "variable ${var:FROM_ENV}" {
		t.Fatalf("VaultNeeds = %v", got)
	}
}

func TestVaultNeeds_SecretSets(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "secrets-vault", "var-sets.yaml")
	stack, err := LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	got := VaultNeeds(stack)
	found := false
	for _, reason := range got {
		if reason == "secrets.sets production" {
			found = true
		}
		if len(reason) >= 12 && reason[:12] == "variable ${" {
			t.Errorf("var-sets reported a variable reference: %s", reason)
		}
	}
	if !found {
		t.Fatalf("VaultNeeds = %v, want secrets.sets production", got)
	}
}

func TestVaultNeeds_CredentialRef(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	body := []byte("version: \"1\"\nname: needs-cred\nmcp-servers:\n  - name: src\n    port: 3000\n    source:\n      type: git\n      url: https://github.com/example/repo.git\n      ref: main\n      auth:\n        method: token\n        credential_ref: \"${vault:GIT_TOKEN}\"\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	got := VaultNeeds(stack)
	if len(got) != 1 || got[0] != "credential_ref ${vault:GIT_TOKEN} on src" {
		t.Fatalf("VaultNeeds = %v", got)
	}
}

func TestVaultNeeds_PlainStack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	body := []byte("version: \"1\"\nname: plain\nmcp-servers:\n  - name: demo\n    command: [\"echo\"]\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := VaultNeeds(stack); len(got) != 0 {
		t.Fatalf("VaultNeeds = %v, want empty", got)
	}
}
