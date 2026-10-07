package mcp

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/logging"
)

func TestReaders_ToolsListChanged(t *testing.T) {
	t.Run("stdio", func(t *testing.T) {
		logger := slog.New(logging.NewBufferHandler(logging.NewLogBuffer(10), nil))
		client := newTestStdioClient("stdio-srv", logger)
		assertReaderHonorsListChanged(t, client.readResponses, client.SetEra, client.setListChangedHandler, func(id int64) chan *jsonrpc.Response {
			ch := make(chan *jsonrpc.Response, 1)
			client.responsesMu.Lock()
			client.responses[id] = ch
			client.responsesMu.Unlock()
			return ch
		})
	})
	t.Run("process", func(t *testing.T) {
		logger := slog.New(logging.NewBufferHandler(logging.NewLogBuffer(10), nil))
		client := newTestProcessClient("process-srv", logger)
		assertReaderHonorsListChanged(t, client.readResponses, client.SetEra, client.setListChangedHandler, func(id int64) chan *jsonrpc.Response {
			ch := make(chan *jsonrpc.Response, 1)
			client.responsesMu.Lock()
			client.responses[id] = ch
			client.responsesMu.Unlock()
			return ch
		})
	})
	t.Run("http", func(t *testing.T) {
		buf := logging.NewLogBuffer(10)
		client := &Client{}
		client.name = "http-srv"
		client.logger = slog.New(logging.NewBufferHandler(buf, nil))
		var called atomic.Int32
		client.SetEra(EraHandshake)
		client.setListChangedHandler(func() { called.Add(1) })
		body := "data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n" +
			"data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"
		resp, err := client.parseSSEResponse(context.Background(), strings.NewReader(body), rawID("1"))
		if err != nil {
			t.Fatal(err)
		}
		if resp.ID == nil || called.Load() != 1 {
			t.Fatalf("handler = %d, resp = %#v", called.Load(), resp)
		}
		entries := buf.GetRecent(10)
		if len(entries) != 1 || entries[0].Message != "server notification handled" || entries[0].Attrs["server"] != "http-srv" || entries[0].Attrs["method"] != MethodToolsListChanged {
			t.Fatalf("log = %#v", entries)
		}

		client.SetEra(EraStateless)
		called.Store(0)
		buf = logging.NewLogBuffer(10)
		client.logger = slog.New(logging.NewBufferHandler(buf, nil))
		if _, err := client.parseSSEResponse(context.Background(), strings.NewReader(body), rawID("1")); err != nil {
			t.Fatal(err)
		}
		if called.Load() != 0 {
			t.Fatal("stateless notification invoked the handler")
		}
		skipped := buf.GetRecent(10)
		if len(skipped) != 1 || skipped[0].Message != "server notification skipped" {
			t.Fatalf("stateless log = %#v", skipped)
		}
	})
}

func assertReaderHonorsListChanged(
	t *testing.T,
	read func(context.Context, io.Reader),
	setEra func(ProtocolEra),
	setHandler func(func()),
	addPending func(int64) chan *jsonrpc.Response,
) {
	t.Helper()
	setEra(EraHandshake)
	var called atomic.Int32
	setHandler(func() { called.Add(1) })
	pending := addPending(7)
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		read(ctx, pr)
	}()
	if _, err := io.WriteString(pw, `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(pw, `{"jsonrpc":"2.0","id":7,"result":{"ok":true}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case resp := <-pending:
		if resp == nil || resp.Error != nil {
			t.Fatalf("response = %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("reader did not deliver the response after the notification")
	}
	deadline := time.Now().Add(time.Second)
	for called.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if called.Load() != 1 {
		t.Fatalf("handler calls = %d", called.Load())
	}

	setEra(EraStateless)
	called.Store(0)
	if _, err := io.WriteString(pw, `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`+"\n"); err != nil {
		t.Fatal(err)
	}
	pending = addPending(8)
	if _, err := io.WriteString(pw, `{"jsonrpc":"2.0","id":8,"result":{"ok":true}}`+"\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-pending:
	case <-time.After(time.Second):
		t.Fatal("stateless reader stalled")
	}
	time.Sleep(20 * time.Millisecond)
	if called.Load() != 0 {
		t.Fatal("stateless notification invoked the handler")
	}
	cancel()
	_ = pw.Close()
	<-done
}
