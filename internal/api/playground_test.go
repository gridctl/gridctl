package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/agent"
)

// stubProvider implements agent.ChatModel for handler tests. The
// stream method emits a fixed sequence of chunks to exercise the
// playground SSE bridge without hitting a real API.
type stubProvider struct {
	chunks []agent.ChatChunk
	err    error
}

func (s *stubProvider) Generate(_ context.Context, _ agent.ChatRequest) (agent.ChatResponse, error) {
	return agent.ChatResponse{}, nil
}

func (s *stubProvider) Stream(_ context.Context, _ agent.ChatRequest) (*agent.StreamReader[agent.ChatChunk], error) {
	if s.err != nil {
		return nil, s.err
	}
	return agent.StreamReaderFromSlice(s.chunks), nil
}

func TestHandlePlaygroundAuth_ReturnsProviderShape(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	r := httptest.NewRequest(http.MethodPost, "/api/playground/auth", nil)
	w := httptest.NewRecorder()
	srv.handlePlaygroundAuth(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp PlaygroundAuthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, name := range []string{"anthropic", "openai", "gemini"} {
		if _, ok := resp.Providers[name]; !ok {
			t.Errorf("Providers map missing %q", name)
		}
	}
	if resp.Ollama.Endpoint == "" {
		t.Errorf("Ollama.Endpoint not set")
	}
}

func TestHandlePlaygroundChat_RejectsMissingFields(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	srv.SetPlaygroundProvider(&stubProvider{})
	body := strings.NewReader(`{"sessionId":""}`)
	r := httptest.NewRequest(http.MethodPost, "/api/playground/chat", body)
	w := httptest.NewRecorder()
	srv.handlePlaygroundChat(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandlePlaygroundChat_RequiresProvider(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	body := strings.NewReader(`{"message":"hi","sessionId":"s","model":"claude-x"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/playground/chat", body)
	w := httptest.NewRecorder()
	srv.handlePlaygroundChat(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (no provider)", w.Code)
	}
	if !strings.Contains(w.Body.String(), "no LLM provider") {
		t.Errorf("body did not mention missing provider: %s", w.Body.String())
	}
}

func TestPlaygroundEndToEnd_StreamsTokensAndDone(t *testing.T) {
	t.Parallel()

	stub := &stubProvider{
		chunks: []agent.ChatChunk{
			{Delta: "hello "},
			{Delta: "world"},
			{Usage: &agent.Usage{InputTokens: 5, OutputTokens: 2}, StopReason: agent.StopReasonEnd},
		},
	}
	srv := &Server{}
	srv.SetPlaygroundProvider(stub)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/playground/chat", srv.handlePlaygroundChat)
	mux.HandleFunc("GET /api/playground/stream", srv.handlePlaygroundStream)
	httpsrv := httptest.NewServer(mux)
	defer httpsrv.Close()

	sessionID := "test-session"

	// Open the stream first (matches frontend ordering) and read in a
	// goroutine so the chat POST can land while the stream is active.
	streamResp, err := http.Get(httpsrv.URL + "/api/playground/stream?sessionId=" + sessionID)
	if err != nil {
		t.Fatalf("stream GET: %v", err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK {
		t.Fatalf("stream status = %d", streamResp.StatusCode)
	}

	type collected struct {
		tokens   string
		gotDone  bool
		gotMetrics bool
	}
	var (
		mu       sync.Mutex
		got      collected
		streamWG sync.WaitGroup
	)
	streamWG.Add(1)
	go func() {
		defer streamWG.Done()
		buf := make([]byte, 4096)
		var pending []byte
		for {
			n, err := streamResp.Body.Read(buf)
			if n > 0 {
				pending = append(pending, buf[:n]...)
				for {
					idx := strings.Index(string(pending), "\n\n")
					if idx < 0 {
						break
					}
					frame := string(pending[:idx])
					pending = pending[idx+2:]
					if !strings.HasPrefix(frame, "data: ") {
						continue
					}
					raw := strings.TrimPrefix(frame, "data: ")
					var ev playgroundEvent
					if err := json.Unmarshal([]byte(raw), &ev); err != nil {
						continue
					}
					mu.Lock()
					switch ev.Type {
					case "token":
						if v, ok := ev.Data["text"].(string); ok {
							got.tokens += v
						}
					case "metrics":
						got.gotMetrics = true
					case "done":
						got.gotDone = true
					}
					mu.Unlock()
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					t.Logf("stream read err: %v", err)
				}
				return
			}
		}
	}()

	// Tiny delay so the stream subscription is visible when chat runs.
	time.Sleep(50 * time.Millisecond)

	chatBody := strings.NewReader(`{"message":"hi","sessionId":"` + sessionID + `","model":"claude-x"}`)
	chatResp, err := http.Post(httpsrv.URL+"/api/playground/chat", "application/json", chatBody)
	if err != nil {
		t.Fatalf("chat POST: %v", err)
	}
	chatResp.Body.Close()
	if chatResp.StatusCode != http.StatusOK {
		t.Fatalf("chat status = %d", chatResp.StatusCode)
	}

	// The stream goroutine returns when the server writes the "done"
	// event and tears down the session — wait for that.
	streamDone := make(chan struct{})
	go func() {
		streamWG.Wait()
		close(streamDone)
	}()
	select {
	case <-streamDone:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not terminate within 5s")
	}

	mu.Lock()
	defer mu.Unlock()
	if got.tokens != "hello world" {
		t.Errorf("tokens = %q, want %q", got.tokens, "hello world")
	}
	if !got.gotMetrics {
		t.Errorf("did not receive metrics event")
	}
	if !got.gotDone {
		t.Errorf("did not receive done event")
	}
}

func TestProviderNameFromModel(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"claude-3-5-haiku-latest": "llm:anthropic",
		"gpt-4o":                  "llm:openai",
		"o1-mini":                 "llm:openai",
		"o3-mini":                 "llm:openai",
		"gemini-2.0-flash":        "llm:google",
		"unknown":                 "llm:unknown",
	}
	for in, want := range cases {
		if got := providerNameFromModel(in); got != want {
			t.Errorf("providerNameFromModel(%q) = %q, want %q", in, got, want)
		}
	}
}
