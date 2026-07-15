package main

import (
	"log/slog"
	"strings"
	"testing"
)

func TestParseLogLevelFlag(t *testing.T) {
	tests := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"debug", slog.LevelDebug, false},
		{"info", slog.LevelInfo, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{"error", slog.LevelError, false},
		{"DEBUG", slog.LevelDebug, false},
		{"bogus", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		got, err := parseLogLevelFlag(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseLogLevelFlag(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if err != nil {
			if !strings.Contains(err.Error(), "--help' for usage") {
				t.Errorf("parseLogLevelFlag(%q) error should carry a usage pointer: %v", tt.in, err)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("parseLogLevelFlag(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestInvalidLogLevelFailsCommand(t *testing.T) {
	_, _, err := executeCommand(t, "version", "--log-level", "bogus")
	if err == nil {
		t.Fatal("--log-level bogus should error")
	}
	if !strings.Contains(err.Error(), "invalid --log-level") {
		t.Errorf("unexpected error: %v", err)
	}
	// Reset the persistent flag so later tests see the default.
	if f := rootCmd.PersistentFlags().Lookup("log-level"); f != nil {
		_ = f.Value.Set("info")
	}
}
