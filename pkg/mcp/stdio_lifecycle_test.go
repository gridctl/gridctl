package mcp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
)

type signaledStdioWriter struct {
	net.Conn
	entered chan struct{}
	once    sync.Once
}

func (w *signaledStdioWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	return w.Conn.Write(data)
}

func blockedStdioFixture(t *testing.T) (*StdioClient, <-chan struct{}) {
	t.Helper()
	writer, reader := net.Pipe()
	t.Cleanup(func() { _ = reader.Close(); _ = writer.Close() })
	signaled := &signaledStdioWriter{Conn: writer, entered: make(chan struct{})}
	client := NewStdioClient("fixture", "fixture", nil)
	client.stdin, client.attached = signaled, true
	return client, signaled.entered
}

func TestStdioClient_CloseInterruptsBlockedWrite(t *testing.T) {
	client, entered := blockedStdioFixture(t)
	written := make(chan error, 1)
	go func() { written <- client.sendStdio(jsonrpc.Request{Method: "ping"}) }()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- client.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("container input write obstructed shutdown")
	}
	select {
	case <-written:
	case <-time.After(time.Second):
		t.Fatal("container input write remained blocked")
	}
}

func TestStdioClient_CancelInterruptsBlockedWrite(t *testing.T) {
	client, entered := blockedStdioFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	written := make(chan error, 1)
	go func() { written <- client.send(ctx, "ping", nil) }()
	<-entered
	cancel()
	select {
	case err := <-written:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt container input")
	}
}
