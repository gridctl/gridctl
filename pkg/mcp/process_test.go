package mcp

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/logging"
)

func TestProcessClient_ReadStderr(t *testing.T) {
	// Test that readStderr reads lines and logs them at WARN level
	buffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(buffer, nil)
	logger := slog.New(handler).With("server", "test-process")

	client := &ProcessClient{
		name:      "test-process",
		logger:    logger,
		responses: make(map[int64]chan *Response),
	}

	// Simulate stderr output
	stderrContent := "error: something failed\nwarning: disk space low\n"
	reader := strings.NewReader(stderrContent)

	// Run readStderr (it will read until EOF)
	done := make(chan struct{})
	go func() {
		client.readStderr(context.Background(), reader)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStderr did not complete in time")
	}

	entries := buffer.GetRecent(10)
	if len(entries) != 2 {
		t.Fatalf("expected 2 log entries, got %d", len(entries))
	}

	// Verify first entry
	if entries[0].Level != "WARN" {
		t.Errorf("expected WARN level, got %s", entries[0].Level)
	}
	if entries[0].Message != "server stderr" {
		t.Errorf("expected message 'server stderr', got %s", entries[0].Message)
	}
	if entries[0].Attrs["output"] != "error: something failed" {
		t.Errorf("expected stderr output in attrs, got %v", entries[0].Attrs["output"])
	}

	// Verify second entry
	if entries[1].Attrs["output"] != "warning: disk space low" {
		t.Errorf("expected stderr output in attrs, got %v", entries[1].Attrs["output"])
	}
}

func TestProcessClient_ReadStderr_Empty(t *testing.T) {
	buffer := logging.NewLogBuffer(10)
	handler := logging.NewBufferHandler(buffer, nil)
	logger := slog.New(handler)

	client := &ProcessClient{
		name:      "test-process",
		logger:    logger,
		responses: make(map[int64]chan *Response),
	}

	// Empty reader should produce no log entries
	reader := bytes.NewReader(nil)
	done := make(chan struct{})
	go func() {
		client.readStderr(context.Background(), reader)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("readStderr did not complete in time")
	}

	entries := buffer.GetRecent(10)
	if len(entries) != 0 {
		t.Errorf("expected 0 log entries for empty stderr, got %d", len(entries))
	}
}
