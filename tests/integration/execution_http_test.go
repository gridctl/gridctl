//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
)

const executionHTTPFixture = `import json
from http.server import BaseHTTPRequestHandler, HTTPServer
class Handler(BaseHTTPRequestHandler):
 def log_message(self, *args): pass
 def do_POST(self):
  request=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))))
  method=request.get('method')
  if 'id' not in request:
   self.send_response(202); self.end_headers(); return
  result={}
  if method=='initialize': result={'protocolVersion':'2025-06-18','capabilities':{'tools':{}},'serverInfo':{'name':'fixture','version':'1'}}
  elif method=='tools/list': result={'tools':[{'name':'echo','inputSchema':{'type':'object','properties':{'message':{'type':'string'}}}}]}
  elif method=='tools/call': result={'content':[{'type':'text','text':request['params']['arguments']['message']}]}
  body=json.dumps({'jsonrpc':'2.0','id':request['id'],'result':result}).encode()
  self.send_response(200); self.send_header('Content-Type','application/json'); self.send_header('Content-Length',str(len(body))); self.end_headers(); self.wfile.write(body)
HTTPServer(('0.0.0.0',8080),Handler).serve_forever()
`

func TestExecution_RealHTTPEndpointBinding(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := dockerruntime.NewWithInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	if err := rt.EnsureImage(ctx, "python:3.13-alpine"); err != nil {
		t.Fatal(err)
	}
	stack := fmt.Sprintf("execution-http-%d", time.Now().UnixNano())
	network := stack + "-net"
	defer func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		exists, id, err := rt.Exists(cleanupCtx, dockerruntime.ContainerName(stack, "fixture"))
		if err != nil {
			t.Error(err)
		} else if exists {
			if err := rt.Remove(cleanupCtx, id); err != nil {
				t.Error(err)
			}
		}
		if err := rt.RemoveNetwork(cleanupCtx, network); err != nil {
			t.Error(err)
		}
	}()
	if err := rt.EnsureNetwork(ctx, network, runtime.NetworkOptions{Driver: "bridge", Stack: stack}); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(65534), uint32(65534)
	server := config.MCPServer{Name: "fixture", Image: "python:3.13-alpine", Transport: "http", Port: 8080, ProtocolGeneration: "handshake", Execution: &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid, Network: "connected"}}
	contract, err := config.ResolveExecution(server)
	if err != nil {
		t.Fatal(err)
	}
	status, err := rt.Start(ctx, runtime.WorkloadConfig{Name: server.Name, Stack: stack, Type: runtime.WorkloadTypeMCPServer, Image: server.Image, Command: []string{"python", "-u", "-c", executionHTTPFixture}, Transport: "http", ExposedPort: 8080, NetworkName: network, Execution: contract})
	if err != nil {
		t.Fatal(err)
	}
	gateway := mcp.NewGateway()
	gateway.SetDockerClient(rt.Client())
	registrar := controller.NewServerRegistrar(gateway, false)
	registrar.SetRuntime(rt)
	if err := registrar.RegisterOne(ctx, server, []controller.ReplicaRuntime{{ContainerID: string(status.ID), HostPort: status.HostPort}}, "stack.yaml"); err != nil {
		t.Fatal(err)
	}
	defer gateway.UnregisterMCPServer(server.Name)
	if got := gateway.Status(); len(got) != 1 || !strings.HasPrefix(got[0].Endpoint, "http://127.0.0.1:") {
		t.Fatal("protected endpoint used ambiguous localhost resolution")
	}
	result, err := gateway.CallTool(ctx, "fixture__echo", map[string]any{"message": "fixture"})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("connected HTTP fixture failed: %v", err)
	}
	gateway.UnregisterMCPServer(server.Name)
	var contacted atomic.Int64
	unrelated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { contacted.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer unrelated.Close()
	endpoint, err := url.Parse(unrelated.URL)
	if err != nil {
		t.Fatal(err)
	}
	wrongPort, err := strconv.Atoi(endpoint.Port())
	if err != nil {
		t.Fatal(err)
	}
	if err := registrar.RegisterOne(ctx, server, []controller.ReplicaRuntime{{ContainerID: string(status.ID), HostPort: wrongPort}}, "stack.yaml"); err == nil {
		t.Fatal("unbound HTTP endpoint became ready")
	}
	if contacted.Load() != 0 {
		t.Fatal("MCP requests reached an endpoint outside the inspected replica")
	}
}
