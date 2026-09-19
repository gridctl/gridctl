package a2aclient

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClient_Close(t *testing.T) {
	closed := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	server.Start()
	defer server.Close()
	client, err := New(Options{Card: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchCard(t.Context()); err != nil {
		t.Fatal(err)
	}
	client.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("idle discovery connection retained after close")
	}
	client.Close()
}
