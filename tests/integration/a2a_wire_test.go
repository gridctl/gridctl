//go:build integration

package integration

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gridctl/gridctl/pkg/a2aclient"
)

// The thin client has no gateway route. These listeners exercise real transport
// and independent wire expectations before a callable adapter can be installed.
func TestA2AWire_Dialects(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					if version == "0.3" {
						_ = json.NewEncoder(w).Encode(map[string]any{"protocolVersion": "0.3.0", "url": "http://" + r.Host + "/rpc"})
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"supportedInterfaces": []any{map[string]string{"protocolVersion": "1.0.1", "protocolBinding": "JSONRPC", "url": "http://" + r.Host + "/rpc"}}})
					}
					return
				}
				requests.Add(1)
				if r.Header.Get("A2A-Version") != version || r.Header.Get("X-Amzn-Bedrock-AgentCore-Runtime-Session-Id") != "" {
					t.Error("incorrect wire headers")
				}
				var request struct {
					ID     string                     `json:"id"`
					Method string                     `json:"method"`
					Params map[string]json.RawMessage `json:"params"`
				}
				if json.NewDecoder(r.Body).Decode(&request) != nil {
					t.Error("invalid request")
					w.WriteHeader(400)
					return
				}
				var result any
				switch request.Method {
				case "SendMessage", "message/send":
					if (version == "1.0") != (request.Method == "SendMessage") {
						t.Error("mixed dialect method")
					}
					var message map[string]any
					if json.Unmarshal(request.Params["message"], &message) != nil {
						t.Error("invalid message")
					}
					var configuration map[string]any
					if json.Unmarshal(request.Params["configuration"], &configuration) != nil {
						t.Error("invalid configuration")
					}
					if version == "1.0" {
						if message["role"] != "ROLE_USER" || configuration["returnImmediately"] != true {
							t.Error("invalid modern send")
						}
					} else if message["role"] != "user" || configuration["blocking"] != false {
						t.Error("invalid legacy send")
					}
					result = map[string]any{"id": "remote-task", "contextId": "remote-context", "status": map[string]string{"state": "working"}}
				case "GetTask", "tasks/get", "CancelTask", "tasks/cancel":
					var id string
					if json.Unmarshal(request.Params["id"], &id) != nil || id != "remote-task" {
						t.Error("lost task routing")
					}
					result = map[string]any{"id": "remote-task", "contextId": "remote-context", "status": map[string]string{"state": "completed"}}
				default:
					t.Error("unsupported RPC method")
					w.WriteHeader(400)
					return
				}
				task := result.(map[string]any)
				if version == "0.3" {
					task["kind"] = "task"
				} else {
					status := task["status"].(map[string]string)
					status["state"] = "TASK_STATE_" + strings.ToUpper(status["state"])
					if request.Method == "SendMessage" {
						result = map[string]any{"task": task}
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
			}))
			defer srv.Close()
			client, err := a2aclient.New(a2aclient.Options{Card: srv.URL + "/card"})
			if err != nil {
				t.Fatal(err)
			}
			cache := a2aclient.NewCardCache(client, "auto")
			card, err := cache.Fetch(t.Context(), false)
			if err != nil {
				t.Fatal(err)
			}
			_, iface, err := a2aclient.ParseCard(card, "auto")
			if err != nil || iface.ProtocolVersion != version {
				t.Fatalf("negotiation: %v", err)
			}
			endpoint, err := client.ResolveEndpoint(iface.URL)
			if err != nil {
				t.Fatal(err)
			}
			for _, op := range []a2aclient.Operation{a2aclient.Send, a2aclient.Get, a2aclient.Cancel} {
				request := a2aclient.Request{ID: "correlation", MessageID: "message", Text: "hello", ReturnImmediately: true, OutputModes: []string{"text/plain"}}
				if op != a2aclient.Send {
					request.TaskID = "remote-task"
				}
				body, err := a2aclient.EncodeRequest(version, op, request)
				if err != nil {
					t.Fatal(err)
				}
				body, status, err := client.RPC(t.Context(), endpoint, version, "", body)
				if err != nil {
					t.Fatal(err)
				}
				result, err := a2aclient.DecodeResponse(version, op, request.ID, status, body)
				if err != nil || result.TaskID != "remote-task" || result.ContextID != "remote-context" {
					t.Fatalf("round trip: %v", err)
				}
			}
			if requests.Load() != 3 {
				t.Fatal("unexpected automatic request or retry")
			}
		})
	}
}

func TestA2AWire_TwoOriginsAndNon2xx(t *testing.T) {
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(entropy[:])
	var calls atomic.Int32
	var mu sync.Mutex
	var discovery string
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Amzn-Bedrock-AgentCore-Runtime-Session-Id") == discovery {
			t.Error("credential or session binding failed")
		}
		w.WriteHeader(409)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":"request","error":{"code":-32054,"message":"Session operation in progress, please retry"}}`)
	}))
	defer rpc.Close()
	card := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		discovery = r.Header.Get("X-Amzn-Bedrock-AgentCore-Runtime-Session-Id")
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("missing card auth")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, rpc.URL, 302)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"protocolVersion": "0.3.0", "url": rpc.URL})
	}))
	defer card.Close()
	client, err := a2aclient.New(a2aclient.Options{Card: card.URL, Token: token, Bedrock: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a2aclient.NewCardCache(client, "auto").Fetch(t.Context(), false); err == nil || calls.Load() != 0 {
		t.Fatal("cross-origin advertisement trusted")
	}
	client, err = a2aclient.New(a2aclient.Options{Card: card.URL + "/redirect", Endpoint: rpc.URL, Token: token, Bedrock: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchCard(t.Context()); err == nil || calls.Load() != 0 {
		t.Fatal("card redirected to authorized RPC origin")
	}
	endpoint, err := client.ResolveEndpoint(card.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(entropy[:16]); err != nil {
		t.Fatal(err)
	}
	entropy[6] = entropy[6]&0x0f | 0x40
	entropy[8] = entropy[8]&0x3f | 0x80
	uuid := hex.EncodeToString(entropy[:16])
	session := uuid[:8] + "-" + uuid[8:12] + "-" + uuid[12:16] + "-" + uuid[16:20] + "-" + uuid[20:]
	body, status, err := client.RPC(t.Context(), endpoint, "0.3", session, []byte(`{"jsonrpc":"2.0","id":"request","method":"tasks/get","params":{"id":"task"}}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = a2aclient.DecodeResponse("0.3", a2aclient.Get, "request", status, body)
	var safe *a2aclient.Error
	if !errors.As(err, &safe) || !safe.Retryable || safe.Status != 409 || safe.Code != -32054 || calls.Load() != 1 {
		t.Fatalf("conflict classification or retry: %v", err)
	}
	if strings.Contains(err.Error(), token) || strings.Contains(err.Error(), session) {
		t.Fatal("unsafe public error")
	}
}
