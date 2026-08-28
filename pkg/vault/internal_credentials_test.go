package vault

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

func TestInternalCredentialKeys_SortedDefensiveCopy(t *testing.T) {
	keys := InternalCredentialKeys()
	if !sort.StringsAreSorted(keys) {
		t.Fatalf("InternalCredentialKeys() = %v, want sorted keys", keys)
	}
	want := []string{
		"GRIDCTL_SSH_KEY_PASSPHRASE",
		"GRIDCTL_VAULT_PASSPHRASE",
		"OP_CONNECT_TOKEN",
		"OP_SERVICE_ACCOUNT_TOKEN",
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("InternalCredentialKeys() = %v, want %v", keys, want)
	}

	keys[0] = "CHANGED"
	if got := InternalCredentialKeys()[0]; got != want[0] {
		t.Fatalf("mutating returned keys changed package state: got %q", got)
	}
}

func TestIsInternalCredential(t *testing.T) {
	tests := map[string]bool{
		"GRIDCTL_VAULT_PASSPHRASE":   true,
		"GRIDCTL_FUTURE_CONTROL_KEY": true,
		"OP_CONNECT_TOKEN":           true,
		"OP_SERVICE_ACCOUNT_TOKEN":   true,
		"GITHUB_TOKEN":               false,
		"gridctl_example":            false,
	}
	for key, want := range tests {
		t.Run(key, func(t *testing.T) {
			if got := IsInternalCredential(key); got != want {
				t.Fatalf("IsInternalCredential(%q) = %v, want %v", key, got, want)
			}
		})
	}
}

func TestInternalCredentialError_Typed(t *testing.T) {
	err := NewInternalCredentialError("GRIDCTL_TEST")
	var target *InternalCredentialError
	if !errors.As(err, &target) || target.Key != "GRIDCTL_TEST" {
		t.Fatalf("NewInternalCredentialError() = %v, want typed error", err)
	}
	if errors.Is(err, NewInternalCredentialError("OTHER")) {
		t.Fatal("errors with different keys must not match")
	}
	if !errors.Is(err, NewInternalCredentialError("GRIDCTL_TEST")) {
		t.Fatal("errors with the same key must match")
	}
}
