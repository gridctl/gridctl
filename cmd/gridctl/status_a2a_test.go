package main

import "testing"

func TestMCPServerType_A2A(t *testing.T) {
	if got := mcpServerType(mcpServerAPI{A2A: true}); got != "a2a" {
		t.Fatalf("type = %s", got)
	}
}
