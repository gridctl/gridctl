package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestColorEnabledNonTTY(t *testing.T) {
	var buf bytes.Buffer
	if ColorEnabled(&buf) {
		t.Error("expected color disabled for a non-TTY writer")
	}
}

func TestColorAllowedByEnv(t *testing.T) {
	tests := []struct {
		name    string
		noColor bool
		env     map[string]string
		want    bool
	}{
		{name: "default allows color", want: true},
		{name: "SetNoColor disables", noColor: true, want: false},
		{name: "NO_COLOR disables", env: map[string]string{"NO_COLOR": "1"}, want: false},
		{name: "NO_COLOR any value disables", env: map[string]string{"NO_COLOR": "true"}, want: false},
		{name: "empty NO_COLOR allows", env: map[string]string{"NO_COLOR": ""}, want: true},
		{name: "TERM dumb disables", env: map[string]string{"TERM": "dumb"}, want: false},
		{name: "TERM xterm allows", env: map[string]string{"TERM": "xterm-256color"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			SetNoColor(tt.noColor)
			t.Cleanup(func() { SetNoColor(false) })

			if got := colorAllowedByEnv(); got != tt.want {
				t.Errorf("colorAllowedByEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHintSuppressedForNonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)
	p.Hint("Try: gridctl apply %s", "stack.yaml")
	if buf.Len() != 0 {
		t.Errorf("expected hint suppressed for non-TTY writer, got %q", buf.String())
	}
}

func TestBannerNonTTYPlain(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriter(&buf)
	p.Banner("v1.2.3")
	out := buf.String()
	if !strings.Contains(out, "gridctl v1.2.3") {
		t.Errorf("expected plain banner, got %q", out)
	}
	if strings.Contains(out, "\033") {
		t.Errorf("expected no ANSI escapes in non-TTY banner, got %q", out)
	}
}
