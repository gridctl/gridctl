package main

import (
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/provisioner"
)

func TestUnknownClientErrorSuggests(t *testing.T) {
	err := unknownClientError(provisioner.NewRegistry(), "claud")
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	if !strings.Contains(msg, `Did you mean "claude"?`) {
		t.Errorf("expected a claude suggestion, got %q", msg)
	}
	if !strings.Contains(msg, "Supported clients:") {
		t.Errorf("expected the supported-clients list, got %q", msg)
	}
}

func TestUnknownClientErrorNoWildGuess(t *testing.T) {
	err := unknownClientError(provisioner.NewRegistry(), "totally-unrelated-thing")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("distance too large for a suggestion, got %q", err)
	}
}
