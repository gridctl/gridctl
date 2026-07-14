package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestJSONAliasRegistered guards against a command gaining resolveFormat
// wiring without the matching --json flag registration (or vice versa).
func TestJSONAliasRegistered(t *testing.T) {
	for _, cmd := range []*cobra.Command{validateCmd, planCmd, optimizeCmd, activateCmd, skillListCmd, varListCmd, statusCmd, infoCmd, doctorCmd, openCmd, pinsListCmd, pinsVerifyCmd} {
		if cmd.Flags().Lookup("json") == nil {
			t.Errorf("command %q is missing its --json flag", cmd.Name())
		}
	}
}

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name          string
		format        string
		formatChanged bool
		asJSON        bool
		want          string
		wantErr       bool
	}{
		{name: "neither set", format: "", want: ""},
		{name: "format json only", format: "json", formatChanged: true, want: "json"},
		{name: "json alias only", asJSON: true, want: "json"},
		{name: "both json", format: "json", formatChanged: true, asJSON: true, want: "json"},
		{name: "alias with unchanged default", format: "table", asJSON: true, want: "json"},
		{name: "conflicting explicit format", format: "yaml", formatChanged: true, asJSON: true, wantErr: true},
		{name: "explicit non-json without alias", format: "yaml", formatChanged: true, want: "yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveFormat(tt.format, tt.formatChanged, tt.asJSON)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveFormat() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("resolveFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}
