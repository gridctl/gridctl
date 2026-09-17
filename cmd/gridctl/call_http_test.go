package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/state"
)

func TestOriginDo_RefusesRedirect(t *testing.T) {
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected = true
		if r.Header.Get("Authorization") != "" {
			t.Error("redirect target received credentials")
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/tools/call", http.StatusFound)
	}))
	defer origin.Close()

	port := origin.Listener.Addr().(*net.TCPAddr).Port
	api := newDaemonAPIFor(state.DaemonState{Port: port, AuthToken: "secret", AuthType: "bearer"}, 0)
	_, err := api.originDo(context.Background(), http.MethodPost, "/api/tools/call", nil, "application/json")
	if !errors.Is(err, errRedirectRefused) {
		t.Fatalf("err = %v", err)
	}
	if redirected {
		t.Fatal("redirect was followed")
	}
}

func TestDecodeCallEnvelope_UseNumberAndTrailing(t *testing.T) {
	body := []byte(`{"schema_version":1,"client":"cli","name":"echo__echo","outcome":{"disposition":"completed","stage":"downstream","reason":"ok","completion":"complete"},"result":{"content":[{"type":"text","text":"ok"}],"_meta":{"n":9007199254740993}},"error":null}`)
	env, err := decodeCallEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := env.Result.Meta["n"].(json.Number)
	if !ok || n.String() != "9007199254740993" {
		t.Fatalf("meta n = %#v", env.Result.Meta["n"])
	}
	if _, err := decodeCallEnvelope(append(body, []byte(" {}")...)); err == nil {
		t.Fatal("trailing document")
	}
	if _, err := decodeCallEnvelope([]byte(`{}`)); err == nil {
		t.Fatal("empty object")
	}
}

func TestDecodeDiscoverEnvelope_RejectsEmpty(t *testing.T) {
	if _, err := decodeDiscoverEnvelope([]byte(`{}`)); err == nil {
		t.Fatal("empty object")
	}
	if _, err := decodeDiscoverEnvelope([]byte(`null`)); err == nil {
		t.Fatal("null")
	}
	ok := []byte(`{"schema_version":1,"client":"cli","tools":[],"total_visible":0,"matched":0,"returned":0,"truncated":false}`)
	got, err := decodeDiscoverEnvelope(ok)
	if err != nil || got.Tools == nil || got.SchemaVersion != 1 {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestClassifyHTTPFailure_TypedNotFound(t *testing.T) {
	body := []byte(`{"schema_version":1,"client":"cli","name":"echo__missing","outcome":{"disposition":"routing_failed","stage":"routing","reason":"not_found","completion":"not_started"},"result":null,"error":{"code":"not_found","message":"tool or server not found"}}`)
	resp := &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"application/json"}}}
	err := classifyHTTPFailure(resp, body, true, "cli", "echo__missing")
	var ce *commandError
	if !errors.As(err, &ce) {
		t.Fatal(err)
	}
	env, ok := ce.envelope.(cliCallEnvelope)
	if !ok || env.Error == nil || env.Error.Code != "not_found" || env.Outcome.Reason != "not_found" {
		t.Fatalf("env = %+v", ce.envelope)
	}
	if env.Outcome.Completion != mcp.CompletionNotStarted {
		t.Fatalf("completion = %s", env.Outcome.Completion)
	}
}

func TestClassifyHTTPFailure_Plain404(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{"Content-Type": []string{"text/plain"}}}
	err := classifyHTTPFailure(resp, []byte("404 page not found"), true, "cli", "echo__echo")
	var ce *commandError
	if !errors.As(err, &ce) {
		t.Fatal(err)
	}
	env := ce.envelope.(cliCallEnvelope)
	if env.Outcome.Reason != reasonMissingEndpoint {
		t.Fatalf("reason = %s", env.Outcome.Reason)
	}
}

func TestReadCappedResponse_OverflowAndReadError(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("a"), callResponseMaxBytes+1)))}
	if _, err := readCappedResponse(resp); !errors.Is(err, errResponseTooLarge) {
		t.Fatalf("overflow err=%v", err)
	}
	resp = &http.Response{Body: io.NopCloser(&errReader{})}
	if _, err := readCappedResponse(resp); err == nil || errors.Is(err, errResponseTooLarge) {
		t.Fatalf("read err=%v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestMapCallTransport_TimeoutUnknown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	err := mapCallTransport(true, "cli", "echo__echo", context.DeadlineExceeded, ctx)
	var ce *commandError
	if !errors.As(err, &ce) {
		t.Fatal(err)
	}
	env := ce.envelope.(cliCallEnvelope)
	if env.Outcome.Completion != mcp.CompletionUnknown || env.Outcome.Reason != reasonDeadlineExceeded || env.Outcome.Disposition != runs.DispositionTimeout {
		t.Fatalf("outcome = %+v", env.Outcome)
	}
}

func TestMapResponseRead_DistinguishesOverflow(t *testing.T) {
	err := mapResponseRead(true, "cli", "echo__echo", errResponseTooLarge)
	env := err.envelope.(cliCallEnvelope)
	if env.Outcome.Reason != reasonResponseTooLarge || env.Outcome.Completion != mcp.CompletionUnknown {
		t.Fatalf("overflow = %+v", env.Outcome)
	}
	err = mapResponseRead(true, "cli", "echo__echo", errors.New("connection reset"))
	env = err.envelope.(cliCallEnvelope)
	if env.Outcome.Reason != reasonTransportError || env.Outcome.Completion != mcp.CompletionUnknown {
		t.Fatalf("read = %+v", env.Outcome)
	}
}

func TestIsJSONContentType(t *testing.T) {
	if !isJSONContentType("application/json; charset=utf-8") {
		t.Fatal("charset")
	}
	if isJSONContentType("text/plain") {
		t.Fatal("plain")
	}
	if isJSONContentType("") {
		t.Fatal("empty")
	}
}
