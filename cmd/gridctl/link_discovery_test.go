package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLink_DiscoveryDryRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRIDCTL_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(resetLinkFlags)

	var out string
	var err error
	stdout := captureStdout(t, func() {
		out, _, err = executeCommand(t, "link", "cursor", "--discovery", "deferred", "--dry-run", "--port", "8180")
	})
	if err != nil {
		t.Fatalf("dry-run: %v\n%s\n%s", err, out, stdout)
	}
	if !strings.Contains(stdout, "discovery=deferred") && !strings.Contains(out, "discovery=deferred") {
		t.Fatalf("dry-run output missing discovery URL:\ncmd: %s\nstdout: %s", out, stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "mcp.json")); err == nil {
		t.Fatal("dry-run wrote mcp.json")
	}

	resetLinkFlags()
	_, _, err = executeCommand(t, "link", "cursor", "--discovery", "lazy", "--dry-run", "--port", "8180")
	if err == nil || !strings.Contains(err.Error(), "--discovery must be deferred or full") {
		t.Fatalf("lazy discovery err = %v", err)
	}
}

func resetLinkFlags() {
	linkPort = 0
	linkAll = false
	linkName = "gridctl"
	linkClientID = ""
	linkGroup = ""
	linkDiscovery = ""
	linkDryRun = false
	linkForce = false
	if linkCmd != nil {
		_ = linkCmd.Flags().Set("port", "0")
		_ = linkCmd.Flags().Set("all", "false")
		_ = linkCmd.Flags().Set("name", "gridctl")
		_ = linkCmd.Flags().Set("client-id", "")
		_ = linkCmd.Flags().Set("group", "")
		_ = linkCmd.Flags().Set("discovery", "")
		_ = linkCmd.Flags().Set("dry-run", "false")
		_ = linkCmd.Flags().Set("force", "false")
	}
}
