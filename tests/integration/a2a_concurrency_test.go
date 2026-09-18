//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

func TestA2AAdapter_CancelDuringResume(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		for _, outcome := range []string{"cancel-first", "send-first", "noncancelable", "conflict", "timeout", "retirement"} {
			t.Run(version+"/"+outcome, func(t *testing.T) {
				ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
				defer stop()
				f := newA2AAdapterFixture(t, version)
				g := mcp.NewGateway()
				defer g.Close()
				if err := g.SetCardPinStorage(ctx, pins.NewWithPath(t.TempDir(), "concurrency")); err != nil {
					t.Fatal(err)
				}
				if err := g.RegisterMCPServer(ctx, mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: f.server.URL, Profile: "bedrock"}}); err != nil {
					t.Fatal(err)
				}
				call := func(name string, args map[string]any) map[string]any {
					t.Helper()
					result, err := g.CallTool(ctx, "agent__"+name, args)
					if err != nil {
						t.Fatal(err)
					}
					return a2aAdapterEnvelope(t, result)
				}
				first := call("send", map[string]any{"message": "start", "return_immediately": true})
				taskArgs := map[string]any{"task_handle": first["task_handle"]}
				resumeArgs := map[string]any{"message": "resume", "context_handle": first["context_handle"], "task_handle": first["task_handle"]}
				sibling := call("send", map[string]any{"message": "new turn", "context_handle": first["context_handle"]})
				if sibling["task_handle"] == nil || sibling["task_handle"] == first["task_handle"] {
					t.Fatal("context-only send did not create new work")
				}
				sendEntered, cancelEntered := make(chan struct{}), make(chan struct{})
				releaseSend, releaseCancel := make(chan struct{}), make(chan struct{})
				var intercept atomic.Bool
				intercept.Store(true)
				f.mu.Lock()
				f.onRPC = func(w http.ResponseWriter, r *http.Request, id, method string, _ map[string]any) bool {
					if !intercept.Load() {
						return false
					}
					var release <-chan struct{}
					switch method {
					case "SendMessage", "message/send":
						close(sendEntered)
						release = releaseSend
					case "CancelTask", "tasks/cancel":
						close(cancelEntered)
						release = releaseCancel
					default:
						t.Error("overlapping get reached the remote")
						return true
					}
					select {
					case <-release:
					case <-r.Context().Done():
						return true
					}
					if method == "CancelTask" || method == "tasks/cancel" {
						switch outcome {
						case "noncancelable", "conflict":
							code, message := -32002, "Task cannot be canceled"
							if outcome == "conflict" {
								code, message = -32054, "Session operation in progress, please retry"
								w.WriteHeader(http.StatusConflict)
							}
							_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
							return true
						case "timeout":
							<-r.Context().Done()
							return true
						}
					}
					return false
				}
				f.mu.Unlock()
				type reply struct {
					result *mcp.ToolCallResult
					err    error
				}
				sendDone, cancelDone := make(chan reply, 1), make(chan reply, 1)
				go func() {
					result, err := g.CallTool(ctx, "agent__send", resumeArgs)
					sendDone <- reply{result, err}
				}()
				wait := func(ch <-chan struct{}) {
					t.Helper()
					select {
					case <-ch:
					case <-ctx.Done():
						t.Fatal("remote operation did not arrive")
					}
				}
				wait(sendEntered)
				before := f.calls.Load()
				for name, args := range map[string]map[string]any{"send": resumeArgs, "task_get": taskArgs, "task_cancel": {"task_handle": sibling["task_handle"]}} {
					if got := call(name, args); got["error"] != "operation_in_progress" {
						t.Fatal("competing operation did not fail fast")
					}
				}
				if f.calls.Load() != before {
					t.Fatal("denied overlap sent an RPC")
				}
				cancelCtx, cancelRequest := context.WithCancel(ctx)
				defer cancelRequest()
				go func() {
					result, err := g.CallTool(cancelCtx, "agent__task_cancel", taskArgs)
					cancelDone <- reply{result, err}
				}()
				wait(cancelEntered)
				if got := call("task_cancel", taskArgs); got["error"] != "operation_in_progress" {
					t.Fatal("second cancel was admitted")
				}
				if outcome == "retirement" {
					g.UnregisterMCPServer("agent")
				} else if outcome == "send-first" {
					close(releaseSend)
				} else {
					close(releaseCancel)
					if outcome == "timeout" {
						cancelRequest()
					}
				}
				var sent, canceled reply
				if outcome == "send-first" {
					sent = <-sendDone
					close(releaseCancel)
					canceled = <-cancelDone
				} else {
					canceled = <-cancelDone
					if outcome != "retirement" {
						close(releaseSend)
					}
					sent = <-sendDone
				}
				if outcome == "retirement" {
					if sent.err == nil && (sent.result == nil || !sent.result.IsError) || canceled.err == nil && (canceled.result == nil || !canceled.result.IsError) {
						t.Fatal("retired generation delivered a successful late response")
					}
					return
				}
				if sent.err != nil || sent.result == nil || !strings.Contains(sent.result.Content[0].Text, "operation_superseded") {
					t.Fatal("older send escaped revision guard")
				}
				if outcome == "timeout" {
					if canceled.err == nil && (canceled.result == nil || !canceled.result.IsError) {
						t.Fatal("local cancellation was reported as remote acknowledgment")
					}
				} else {
					if canceled.err != nil {
						t.Fatal(canceled.err)
					}
					got := a2aAdapterEnvelope(t, canceled.result)
					if outcome == "noncancelable" || outcome == "conflict" {
						if !canceled.result.IsError || got["retryable"] != (outcome == "conflict") {
							t.Fatal("remote cancel refusal lost its classification")
						}
					} else if got["state"] != "canceled" || got["context_handle"] != nil {
						t.Fatal("cancel acknowledgment leaked context or claimed the wrong state")
					}
				}
				intercept.Store(false)
				if got := call("send", resumeArgs); got["error"] != "operation_uncertain" {
					t.Fatal("overlap allowed mutation before reconciliation")
				}
				if got := call("task_get", taskArgs); got["task_handle"] != first["task_handle"] {
					t.Fatal("post-drain reconciliation lost task authority")
				}
				got := call("send", resumeArgs)
				if outcome == "send-first" || outcome == "cancel-first" {
					if got["error"] != "capability_unavailable" {
						t.Fatal("late resume revived a terminal task")
					}
				} else if got["task_handle"] != first["task_handle"] {
					t.Fatal("interrupted get did not reconcile a refused cancel")
				}
			})
		}
	}
}
