package main

import (
	"strings"
	"testing"
)

func TestResolvePlainRejectsJSON(t *testing.T) {
	tests := []struct {
		name    string
		plain   bool
		format  string
		wantErr bool
	}{
		{"plain alone", true, "", false},
		{"plain with table format", true, "table", false},
		{"plain with json", true, "json", true},
		{"plain with JSON case-insensitive", true, "JSON", true},
		{"json alone", false, "json", false},
		{"neither", false, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := resolvePlain(tt.plain, tt.format)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolvePlain(%v, %q) error = %v, wantErr %v", tt.plain, tt.format, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "--plain") {
				t.Errorf("error should name --plain: %v", err)
			}
		})
	}
}

func TestStatusRejectsPlainWithJSON(t *testing.T) {
	_, _, err := executeCommand(t, "status", "--plain", "--json")
	if err == nil {
		t.Fatal("status --plain --json should error")
	}
	if !strings.Contains(err.Error(), "--plain") || !strings.Contains(err.Error(), "--json") {
		t.Errorf("error should name both flags: %v", err)
	}
}

// TestPlainFlagRegistered guards against a table command missing the
// --plain flag (the F13 contract set).
func TestPlainFlagRegistered(t *testing.T) {
	for _, cmd := range []struct {
		name string
		args []string
	}{
		{"status", []string{"status"}},
		{"skill list", []string{"skill", "list"}},
		{"pins list", []string{"pins", "list"}},
		{"optimize", []string{"optimize"}},
		{"telemetry status", []string{"telemetry", "status"}},
	} {
		target, _, err := rootCmd.Find(cmd.args)
		if err != nil {
			t.Fatalf("finding %s: %v", cmd.name, err)
		}
		if target.Flags().Lookup("plain") == nil {
			t.Errorf("%s is missing the --plain flag", cmd.name)
		}
	}

	// In the var family --plain already means "unmask" (var get, var
	// export); the formatting flag must NOT exist there or the two
	// meanings collide into a credential leak.
	varList, _, err := rootCmd.Find([]string{"var", "list"})
	if err != nil {
		t.Fatalf("finding var list: %v", err)
	}
	if varList.Flags().Lookup("plain") != nil {
		t.Error("var list must not carry the table --plain flag (collides with the unmask meaning)")
	}
}
