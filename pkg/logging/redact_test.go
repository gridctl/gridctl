package logging

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "bearer token",
			input:    "connecting with Bearer eyJhbGciOiJIUzI1NiJ9.secret",
			contains: "Bearer [REDACTED]",
			excludes: "eyJhbGciOiJIUzI1NiJ9",
		},
		{
			name:     "bearer token case insensitive",
			input:    "header bearer sk-abc123xyz",
			contains: "bearer [REDACTED]",
			excludes: "sk-abc123xyz",
		},
		{
			name:     "authorization header with bearer",
			input:    "Authorization: Bearer MDI1ZWZhOTktZGNkZC00OWI3",
			contains: "[REDACTED]",
			excludes: "MDI1ZWZhOTktZGNkZC00OWI3",
		},
		{
			name:     "authorization header without bearer",
			input:    "Authorization: Basic dXNlcjpwYXNz",
			contains: "Authorization: [REDACTED]",
			excludes: "dXNlcjpwYXNz",
		},
		{
			name:     "password pattern",
			input:    "connecting with password=mysecretpass123",
			contains: "password=[REDACTED]",
			excludes: "mysecretpass123",
		},
		{
			name:     "api key pattern",
			input:    "using api_key=abcdef12345",
			contains: "api_key=[REDACTED]",
			excludes: "abcdef12345",
		},
		{
			name:     "token pattern",
			input:    "set token=ghp_xxxxxxxxxxxx",
			contains: "token=[REDACTED]",
			excludes: "ghp_xxxxxxxxxxxx",
		},
		{
			name:     "non-sensitive value unchanged",
			input:    "registering MCP server name=github port=9000",
			contains: "name=github port=9000",
		},
		{
			name:     "empty string unchanged",
			input:    "",
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactString(tt.input)
			if tt.contains != "" && !strings.Contains(result, tt.contains) {
				t.Errorf("expected result to contain %q, got %q", tt.contains, result)
			}
			if tt.excludes != "" && strings.Contains(result, tt.excludes) {
				t.Errorf("expected result to NOT contain %q, got %q", tt.excludes, result)
			}
		})
	}
}

func TestRedactingHandler_Message(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler)

	logger.Info("connecting with Bearer eyJtoken123")

	output := buf.String()
	if strings.Contains(output, "eyJtoken123") {
		t.Errorf("expected token to be redacted from message, got: %s", output)
	}
	if !strings.Contains(output, "Bearer [REDACTED]") {
		t.Errorf("expected redacted message, got: %s", output)
	}
}

func TestRedactingHandler_StringAttr(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler)

	logger.Info("request", "header", "Authorization: Bearer sk-secret-value")

	output := buf.String()
	if strings.Contains(output, "sk-secret-value") {
		t.Errorf("expected secret to be redacted from attr, got: %s", output)
	}
}

func TestRedactingHandler_StringSliceAttr(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler)

	cmd := []string{"npx", "mcp-remote", "https://api.example.com", "--header", "Authorization: Bearer MDI1ZWZhOTk"}
	logger.Info("registering server", "command", cmd)

	output := buf.String()
	if strings.Contains(output, "MDI1ZWZhOTk") {
		t.Errorf("expected bearer token to be redacted from command array, got: %s", output)
	}
}

func TestRedactingHandler_WithAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler).With("auth", "Bearer persistent-token")

	logger.Info("test")

	output := buf.String()
	if strings.Contains(output, "persistent-token") {
		t.Errorf("expected persistent attr to be redacted, got: %s", output)
	}
}

func TestRedactingHandler_WithGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler).WithGroup("config")

	logger.Info("loaded", "secret", "password=abc123")

	output := buf.String()
	if strings.Contains(output, "abc123") {
		t.Errorf("expected grouped attr to be redacted, got: %s", output)
	}
}

func TestRedactingHandler_NonSensitivePassthrough(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler)

	logger.Info("MCP server listening", "name", "github", "port", 9000)

	output := buf.String()
	if !strings.Contains(output, "github") {
		t.Errorf("expected non-sensitive value to pass through, got: %s", output)
	}
	if !strings.Contains(output, "9000") {
		t.Errorf("expected non-sensitive int to pass through, got: %s", output)
	}
}

func TestRedactingHandler_Enabled(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewRedactingHandler(inner)

	if handler.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected debug to be disabled when inner is WARN")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected warn to be enabled when inner is WARN")
	}
}

func TestRedactingHandler_MapAttr(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler)

	env := map[string]string{
		"API_KEY":   "sk-secret123",
		"LOG_LEVEL": "debug",
	}
	logger.Info("config", "env", env)

	output := buf.String()
	if strings.Contains(output, "sk-secret123") {
		t.Errorf("expected map value to be redacted, got: %s", output)
	}
	if !strings.Contains(output, "debug") {
		t.Errorf("expected non-sensitive map value to pass through, got: %s", output)
	}
}

func TestRedactingHandler_ErrorAttr(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	handler := NewRedactingHandler(inner)
	logger := slog.New(handler)

	logger.Error("auth failed", "error", fmt.Errorf("invalid Bearer eyJsecret123"))

	output := buf.String()
	if strings.Contains(output, "eyJsecret123") {
		t.Errorf("expected error message to be redacted, got: %s", output)
	}
}

func TestRedactEnv(t *testing.T) {
	env := map[string]string{
		"GITHUB_TOKEN":       "ghp_secret123",
		"API_KEY":            "sk-abc",
		"LOG_LEVEL":          "debug",
		"POSTGRES_PASSWORD":  "p@ssw0rd",
		"DATABASE_URL":       "postgres://localhost:5432/db",
		"AUTH_TOKEN":         "bearer-xyz",
	}

	redacted := RedactEnv(env)

	if redacted["GITHUB_TOKEN"] != "[REDACTED]" {
		t.Errorf("expected GITHUB_TOKEN redacted, got %q", redacted["GITHUB_TOKEN"])
	}
	if redacted["API_KEY"] != "[REDACTED]" {
		t.Errorf("expected API_KEY redacted, got %q", redacted["API_KEY"])
	}
	if redacted["LOG_LEVEL"] != "debug" {
		t.Errorf("expected LOG_LEVEL unchanged, got %q", redacted["LOG_LEVEL"])
	}
	if redacted["POSTGRES_PASSWORD"] != "[REDACTED]" {
		t.Errorf("expected POSTGRES_PASSWORD redacted, got %q", redacted["POSTGRES_PASSWORD"])
	}
	if redacted["DATABASE_URL"] != "postgres://localhost:5432/db" {
		t.Errorf("expected DATABASE_URL unchanged, got %q", redacted["DATABASE_URL"])
	}
	if redacted["AUTH_TOKEN"] != "[REDACTED]" {
		t.Errorf("expected AUTH_TOKEN redacted, got %q", redacted["AUTH_TOKEN"])
	}
}

func TestRedactEnv_Nil(t *testing.T) {
	if RedactEnv(nil) != nil {
		t.Error("expected nil for nil input")
	}
}
