package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/state"
)

func TestPrintTailLastN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\nfive\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var buf bytes.Buffer
	if _, err := printTail(&buf, f, 3); err != nil {
		t.Fatalf("printTail: %v", err)
	}
	got := buf.String()
	if got != "three\nfour\nfive\n" {
		t.Errorf("printTail = %q, want last three lines", got)
	}
}

func TestPrintTailNonPositiveMeansAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "all.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, n := range []int{0, -1} {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if _, err := printTail(&buf, f, n); err != nil {
			t.Fatalf("printTail(n=%d): %v", n, err)
		}
		f.Close()
		if buf.String() != "one\ntwo\nthree\n" {
			t.Errorf("printTail(n=%d) = %q, want all lines", n, buf.String())
		}
	}
}

func TestPrintTailHandlesVeryLongLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "long.log")
	long := strings.Repeat("x", 2*1024*1024)
	if err := os.WriteFile(path, []byte("before\n"+long+"\nafter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var buf bytes.Buffer
	if _, err := printTail(&buf, f, 2); err != nil {
		t.Fatalf("printTail with a 2MB line: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "after\n") {
		t.Error("expected the tail to include the final line after a long line")
	}
}

func TestPrintTailFewerLinesThanRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "short.log")
	if err := os.WriteFile(path, []byte("only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var buf bytes.Buffer
	if _, err := printTail(&buf, f, 100); err != nil {
		t.Fatalf("printTail: %v", err)
	}
	if buf.String() != "only\n" {
		t.Errorf("printTail = %q, want %q", buf.String(), "only\n")
	}
}

func TestFollowLogSeesAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "follow.log")
	if err := os.WriteFile(path, []byte("start\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	info, _ := f.Stat()
	offset := info.Size()

	go func() {
		time.Sleep(100 * time.Millisecond)
		appendFile, appendErr := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
		if appendErr != nil {
			return
		}
		defer appendFile.Close()
		_, _ = appendFile.WriteString("appended\n")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	var buf bytes.Buffer
	if err := followLog(ctx, &buf, f, offset); err != nil {
		t.Fatalf("followLog: %v", err)
	}
	if !strings.Contains(buf.String(), "appended") {
		t.Errorf("followLog missed appended data, got %q", buf.String())
	}
}

func TestResolveLogsStackNoneRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := resolveLogsStack("")
	if err == nil {
		t.Fatal("expected an error with no running stacks")
	}
	if !strings.Contains(err.Error(), "gridctl apply") {
		t.Errorf("error should suggest the next step, got %q", err)
	}
}

func TestResolveLogsStackSingleRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	st := &state.DaemonState{StackName: "solo", StackFile: "/x.yaml", PID: os.Getpid(), Port: 8181, StartedAt: time.Now()}
	if err := state.Save(st); err != nil {
		t.Fatal(err)
	}

	name, err := resolveLogsStack("")
	if err != nil {
		t.Fatalf("resolveLogsStack: %v", err)
	}
	if name != "solo" {
		t.Errorf("name = %q, want solo", name)
	}
}

func TestResolveLogsStackExplicitUnknown(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	_, err := resolveLogsStack("ghost")
	if err == nil {
		t.Fatal("expected an error for an unknown stack")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want not found", err)
	}
}

func TestResolveLogsStackLeftoverLogFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := state.EnsureLogDir(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.LogPath("old"), []byte("history\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	name, err := resolveLogsStack("old")
	if err != nil {
		t.Fatalf("resolveLogsStack: %v", err)
	}
	if name != "old" {
		t.Errorf("name = %q, want old", name)
	}
}
