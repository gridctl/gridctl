package controller

import (
	"log/slog"
	"strings"
	"testing"
)

func TestEffectiveLogLevel(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want slog.Level
	}{
		{"default is info", Config{}, slog.LevelInfo},
		{"explicit debug", Config{LogLevel: slog.LevelDebug}, slog.LevelDebug},
		{"explicit error", Config{LogLevel: slog.LevelError}, slog.LevelError},
		{"verbose lowers default to debug", Config{Verbose: true}, slog.LevelDebug},
		{"verbose raises explicit error to debug", Config{Verbose: true, LogLevel: slog.LevelError}, slog.LevelDebug},
		{"verbose with explicit debug stays debug", Config{Verbose: true, LogLevel: slog.LevelDebug}, slog.LevelDebug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectiveLogLevel(tt.cfg); got != tt.want {
				t.Errorf("effectiveLogLevel(%+v) = %v, want %v", tt.cfg, got, tt.want)
			}
		})
	}
}

func TestAppendLogLevelArg(t *testing.T) {
	if got := appendLogLevelArg([]string{"apply"}, slog.LevelInfo); len(got) != 1 {
		t.Errorf("info level should not be forwarded, got %v", got)
	}
	got := appendLogLevelArg([]string{"apply"}, slog.LevelDebug)
	if len(got) != 3 || got[1] != "--log-level" || got[2] != "debug" {
		t.Errorf("debug level not forwarded correctly: %v", got)
	}
	got = appendLogLevelArg(nil, slog.LevelWarn)
	if strings.Join(got, " ") != "--log-level warn" {
		t.Errorf("warn level not forwarded correctly: %v", got)
	}
}
