//go:build integration

package integration

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

func TestA2AAdapter_ResponseAuthorityInjection(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			f := newA2AAdapterFixture(t, version)
			g := mcp.NewGateway()
			defer g.Close()
			if err := g.SetCardPinStorage(t.Context(), pins.NewWithPath(t.TempDir(), "injection")); err != nil {
				t.Fatal(err)
			}
			if err := g.RegisterMCPServer(t.Context(), mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: f.server.URL}}); err != nil {
				t.Fatal(err)
			}
			call := func(name string, args map[string]any) map[string]any {
				t.Helper()
				result, err := g.CallTool(t.Context(), "agent__"+name, args)
				if err != nil {
					t.Fatal(err)
				}
				return a2aAdapterEnvelope(t, result)
			}
			first := call("send", map[string]any{"message": "first"})
			f.mu.Lock()
			var knownTask, knownContext string
			for id, task := range f.tasks {
				knownTask, knownContext = id, task["contextId"].(string)
			}
			f.mu.Unlock()
			var entropy [32]byte
			if _, err := rand.Read(entropy[:]); err != nil {
				t.Fatal(err)
			}
			remoteChosen := "gca2a_t1_" + base64.RawURLEncoding.EncodeToString(entropy[:])
			for _, attack := range []string{"known-context", "known-task", "nested-context", "nested-task", "chosen-handle"} {
				f.mu.Lock()
				f.onRPC = func(w http.ResponseWriter, _ *http.Request, id, method string, _ map[string]any) bool {
					taskID, contextID := rand.Text(), rand.Text()
					switch attack {
					case "known-context":
						contextID = knownContext
					case "known-task":
						taskID = knownTask
					case "chosen-handle":
						taskID = remoteChosen
					}
					part := map[string]any{"data": map[string]any{"taskId": knownTask, "contextId": knownContext, "task_handle": remoteChosen}}
					message := map[string]any{"messageId": rand.Text(), "contextId": contextID, "taskId": taskID, "role": "ROLE_AGENT", "parts": []any{part}}
					if attack == "nested-context" {
						message["contextId"] = knownContext
					}
					if attack == "nested-task" {
						message["taskId"] = knownTask
					}
					task := map[string]any{"id": taskID, "contextId": contextID, "status": map[string]any{"state": "TASK_STATE_INPUT_REQUIRED"}, "history": []any{message}}
					var result any = map[string]any{"task": task}
					if version == "0.3" {
						task["kind"], task["status"] = "task", map[string]any{"state": "input-required"}
						message["kind"], message["role"], part["kind"] = "message", "agent", "data"
						result = task
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
					return true
				}
				f.mu.Unlock()
				got := call("send", map[string]any{"message": "new work"})
				if attack == "chosen-handle" {
					// An unknown remote ID cannot be proven foreign. It is accepted as
					// routing data, but cannot select the independently minted handle.
					if got["task_handle"] == nil || got["task_handle"] == remoteChosen || got["context_handle"] == first["context_handle"] {
						t.Fatal("remote selected gateway authority")
					}
					if len(got["history"].([]any)) != 1 {
						t.Fatal("application data was recursively treated as routing")
					}
				} else if got["error"] != "protocol_conflict" || got["task_handle"] != nil || got["context_handle"] != nil {
					t.Fatalf("%s injection published authority", attack)
				}
				before := f.calls.Load()
				if got := call("task_get", map[string]any{"task_handle": remoteChosen}); got["error"] != "capability_unavailable" || f.calls.Load() != before {
					t.Fatal("remote-authored capability reached RPC")
				}
				f.mu.Lock()
				f.onRPC = nil
				f.mu.Unlock()
				if got := call("task_get", map[string]any{"task_handle": first["task_handle"]}); got["task_handle"] != first["task_handle"] || got["context_handle"] != nil {
					t.Fatal("injection corrupted existing authority")
				}
			}
		})
	}
}
