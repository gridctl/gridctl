package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

func TestProcessLifecycleHelper(t *testing.T) {
	mode := os.Getenv("PROCESS_LIFECYCLE_FIXTURE")
	if mode == "" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var req jsonrpc.Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			os.Exit(2)
		}
		if req.ID == nil {
			continue
		}
		result := `{}`
		switch req.Method {
		case "initialize":
			if mode == "reject" {
				fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"error\":{\"code\":-32600,\"message\":\"fixture rejection\"}}\n", *req.ID)
				continue
			}
			result = `{"protocolVersion":"2025-03-26","capabilities":{},"serverInfo":{"name":"fixture","version":"1"}}`
		case "tools/list":
			result = `{"tools":[]}`
		}
		fmt.Printf("{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":%s}\n", *req.ID, result)
		if mode == "exit-after-response" {
			os.Exit(0)
		}
	}
	os.Exit(0)
}

func lifecycleFixture(t *testing.T, mode string) *ProcessClient {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := NewProcessClient("fixture", []string{executable, "-test.run=^TestProcessLifecycleHelper$"}, "", map[string]string{"PROCESS_LIFECYCLE_FIXTURE": mode})
	c.SetGenerationPin(GenerationHandshake)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestProcessClient_ReconnectOwnsReplacement(t *testing.T) {
	c := lifecycleFixture(t, "serve")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	oldProcess := c.cmd
	if err := c.Reconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if oldProcess.ProcessState == nil || c.cmd == oldProcess || c.PID() == 0 {
		t.Fatal("reconnect did not reap and replace the old child")
	}
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestProcessClient_FailedReconnectClosesReplacement(t *testing.T) {
	c := lifecycleFixture(t, "reject")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := c.Reconnect(ctx); err == nil {
		t.Fatal("fixture unexpectedly accepted initialization")
	}
	if c.PID() != 0 || c.cmd.ProcessState == nil {
		t.Fatal("failed reconnect left an unowned child")
	}
}

func TestProcessClient_PreservesFinalResponseAtExit(t *testing.T) {
	c := lifecycleFixture(t, "exit-after-response")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.call(ctx, "ping", nil, nil); err != nil {
		t.Fatalf("final response was lost: %v", err)
	}
}

func TestProcessClient_ReapNaturalExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	c := NewProcessClient("exit", []string{"/bin/sh", "-c", "exit 0"}, "", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	deadline := time.Now().Add(3 * time.Second)
	for c.PID() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if c.PID() != 0 {
		t.Fatal("exited child still has a live PID")
	}
}

func TestProcessClient_CloseUnblocksPipeWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	c := NewProcessClient("blocked", []string{"/bin/sh", "-c", "exec sleep 30"}, "", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.cmd.Process.Kill()
		_ = c.Close()
	})
	written := make(chan error, 1)
	go func() {
		written <- c.sendStdio(jsonrpc.Request{JSONRPC: "2.0", Method: strings.Repeat("x", 1024*1024)})
	}()
	time.Sleep(50 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("Close blocked behind stdin write")
	}
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("stdin write leaked after shutdown")
	}
	if c.PID() != 0 {
		t.Fatal("closed child still has a live PID")
	}
}

func TestProcessClient_CancelBlockedPipeWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	c := NewProcessClient("cancel-write", []string{"/bin/sh", "-c", "exec sleep 30"}, "", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	writeCtx, stopWrite := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stopWrite()
	started := time.Now()
	err := c.sendStdioContext(writeCtx, jsonrpc.Request{JSONRPC: "2.0", Method: strings.Repeat("x", 1024*1024)})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("write returned %v, want deadline exceeded", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("write ignored cancellation")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessClient_CancelReapsChild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	c := NewProcessClient("cancel-child", []string{"/bin/sh", "-c", "exec sleep 30"}, "", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	cancel()
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled process was not reaped")
	}
	if c.PID() != 0 || c.cmd.ProcessState == nil {
		t.Fatal("canceled process retained a live PID or lacked exit state")
	}
}

func TestProcessClient_ConnectAfterClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture")
	}
	c := NewProcessClient("restart", []string{"/bin/cat"}, "", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = c.Close() })
	for range 3 {
		if err := c.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		if c.PID() == 0 {
			t.Fatal("started child has no PID")
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if c.PID() != 0 {
			t.Fatal("closed child still has a live PID")
		}
		if c.cmd.ProcessState == nil {
			t.Fatal("closed child was not waited")
		}
	}
}
