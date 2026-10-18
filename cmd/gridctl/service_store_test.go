package main

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/vault"
)

func lockTestStore(t *testing.T, dir string) bool {
	t.Helper()
	store := vault.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("TOKEN", "value"); err != nil {
		t.Fatal(err)
	}
	if err := store.Lock("secret"); err != nil {
		t.Fatal(err)
	}
	locked := vault.NewStore(dir)
	if err := locked.Load(); err != nil {
		t.Fatal(err)
	}
	return locked.IsLocked()
}
