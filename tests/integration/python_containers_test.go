//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/gridctl/gridctl/pkg/builder"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker"
)

const pythonFixtureModule = `import json
import sys


def reply(message):
    sys.stdout.write(json.dumps(message) + "\n")
    sys.stdout.flush()


def main():
    for line in sys.stdin:
        request = json.loads(line)
        if "id" not in request:
            continue
        method = request.get("method")
        if method == "server/discover":
            reply({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}})
        elif method == "initialize":
            version = request.get("params", {}).get("protocolVersion", "2025-06-18")
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"protocolVersion": version, "capabilities": {"tools": {}}, "serverInfo": {"name": "python-fixture", "version": "1.0"}}})
        elif method == "tools/list":
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"tools": [{"name": "echo", "description": "Echo a message", "inputSchema": {"type": "object", "properties": {"message": {"type": "string"}}, "required": ["message"]}}]}})
        elif method == "tools/call":
            message = request.get("params", {}).get("arguments", {}).get("message", "")
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {"content": [{"type": "text", "text": message}]}})
        elif method == "ping":
            reply({"jsonrpc": "2.0", "id": request["id"], "result": {}})
        else:
            reply({"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "Method not found"}})
`

func TestPythonContainers_RealRuntime(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	orchestrator, err := runtime.NewWithInfo(info)
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	defer orchestrator.Close() //nolint:errcheck

	t.Run("public PyPI package builds and serves stdio", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()

		stack := pythonStack("inttest-python-pypi", config.MCPServer{
			Name: "fetch",
			Source: &config.Source{
				Type:    "pypi",
				Package: "mcp-server-fetch",
				Ref:     "2026.8.18",
			},
			Transport: "stdio",
		})
		cleanupStack(t, ctx, orchestrator, stack.Name)

		result, err := orchestrator.Up(ctx, stack, runtime.UpOptions{})
		if err != nil {
			t.Fatalf("Up(PyPI): %v", err)
		}
		server := onlyServer(t, result)
		if server.Image == "" || strings.HasSuffix(server.Image, ":latest") {
			t.Fatalf("resolved PyPI image = %q, want versioned image", server.Image)
		}
		client := connectStdio(t, ctx, orchestrator, server.Replicas[0].WorkloadID)
		if !hasTool(client.Tools(), "fetch") {
			t.Fatalf("PyPI tools = %v, want fetch", toolNames(client.Tools()))
		}
		call, err := client.CallTool(ctx, "fetch", map[string]any{"url": "https://example.com"})
		if err != nil {
			t.Fatalf("PyPI fetch tool call: %v", err)
		}
		if call.IsError {
			t.Fatalf("PyPI fetch tool returned an error: %+v", call.Content)
		}
	})

	t.Run("pinned Git source covers lifecycle paths", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
		defer cancel()

		repository, firstCommit := newPythonGitFixture(t)
		stackName := "inttest-python-git"
		server := gitPythonServer(repository, firstCommit.String())
		stack := pythonStack(stackName, server)
		cleanupStack(t, ctx, orchestrator, stackName)

		imageBuilder := builder.New(orchestrator.DockerClient())
		buildOptions := pythonBuildOptions(stackName, server)
		firstBuild, err := imageBuilder.Build(ctx, buildOptions)
		if err != nil {
			t.Fatalf("first Git build: %v", err)
		}
		if firstBuild.Cached {
			t.Fatal("first Git build unexpectedly reported a cache hit")
		}
		cachedBuild, err := imageBuilder.Build(ctx, buildOptions)
		if err != nil {
			t.Fatalf("cached Git build: %v", err)
		}
		if !cachedBuild.Cached || cachedBuild.ImageTag != firstBuild.ImageTag {
			t.Fatalf("cached build = %+v, want cached %q", cachedBuild, firstBuild.ImageTag)
		}

		result, err := orchestrator.Up(ctx, stack, runtime.UpOptions{})
		if err != nil {
			t.Fatalf("Up(Git replicas): %v", err)
		}
		built := onlyServer(t, result)
		if len(built.Replicas) != 2 {
			t.Fatalf("Git replicas = %d, want 2", len(built.Replicas))
		}
		if built.Image != firstBuild.ImageTag {
			t.Fatalf("runtime image = %q, want cached build %q", built.Image, firstBuild.ImageTag)
		}
		for _, replica := range built.Replicas {
			if replica.HostPort != 0 {
				t.Errorf("stdio replica %d published host port %d", replica.ReplicaID, replica.HostPort)
			}
		}
		assertEchoCall(t, ctx, connectStdio(t, ctx, orchestrator, built.Replicas[0].WorkloadID), "first commit")

		secondCommit := updatePythonGitFixture(t, repository, "0.2.0")
		updatedServer := gitPythonServer(repository, secondCommit.String())
		updatedStack := pythonStack(stackName, updatedServer)
		updated, err := orchestrator.Up(ctx, updatedStack, runtime.UpOptions{})
		if err != nil {
			t.Fatalf("Up(updated Git ref): %v", err)
		}
		updatedResult := onlyServer(t, updated)
		if updatedResult.Image == built.Image {
			t.Fatalf("Git ref update retained image %q", built.Image)
		}
		for i := range updatedResult.Replicas {
			if updatedResult.Replicas[i].WorkloadID == built.Replicas[i].WorkloadID {
				t.Errorf("replica %d retained stale workload %s", i, built.Replicas[i].WorkloadID)
			}
		}

		thirdCommit := updatePythonGitFixture(t, repository, "0.3.0")
		reloadedServer := gitPythonServer(repository, thirdCommit.String())
		reloadedStack := pythonStack(stackName, reloadedServer)
		thirdPlan, err := imageBuilder.Plan(ctx, pythonBuildOptions(stackName, reloadedServer))
		if err != nil {
			t.Fatalf("plan third Git commit: %v", err)
		}
		expectedReloadImage := thirdPlan.ImageTag
		if err := thirdPlan.Close(); err != nil {
			t.Fatalf("close third Git plan: %v", err)
		}
		stackPath := filepath.Join(t.TempDir(), "stack.yaml")
		writeStackYAML(t, stackPath, updatedStack)
		gateway := mcp.NewGateway()
		defer gateway.Close()
		handler := reload.NewHandler(stackPath, updatedStack, gateway, orchestrator, 0, 0, nil, nil)
		handler.SetRegisterServerFunc(func(context.Context, config.MCPServer, []reload.ReplicaRuntime, string) error { return nil })
		writeStackYAML(t, stackPath, reloadedStack)
		reloadResult, err := handler.Reload(ctx)
		if err != nil {
			t.Fatalf("Reload(updated Git ref): %v", err)
		}
		if !reloadResult.Success || len(reloadResult.Modified) != 1 {
			t.Fatalf("reload result = %+v, want one successful modification", reloadResult)
		}
		statuses, err := orchestrator.Status(ctx, stackName)
		if err != nil {
			t.Fatalf("Status(after reload): %v", err)
		}
		if len(statuses) != 2 {
			t.Fatalf("reload statuses = %+v, want two containers", statuses)
		}
		for _, listed := range statuses {
			status, err := orchestrator.Runtime().Status(ctx, listed.ID)
			if err != nil {
				t.Fatalf("Status(%s after reload): %v", listed.ID, err)
			}
			if status.Image != expectedReloadImage || status.HostPort != 0 {
				t.Errorf("reloaded workload = %+v, want image %q and host port 0", status, expectedReloadImage)
			}
		}

		testPythonAutoscaler(t, ctx, orchestrator, expectedReloadImage)
	})
}

func pythonStack(name string, server config.MCPServer) *config.Stack {
	return &config.Stack{
		Version:    "1",
		Name:       name,
		Network:    config.Network{Name: name + "-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{server},
	}
}

func gitPythonServer(repository, ref string) config.MCPServer {
	return config.MCPServer{
		Name:      "fixture",
		Transport: "stdio",
		Replicas:  2,
		Source: &config.Source{
			Type:    "git",
			URL:     repository,
			Ref:     ref,
			Path:    "services/fixture",
			Runtime: "python",
		},
	}
}

func pythonBuildOptions(stackName string, server config.MCPServer) builder.BuildOptions {
	return builder.BuildOptions{
		Stack: stackName, ServerName: server.Name, SourceType: server.Source.Type,
		URL: server.Source.URL, Ref: server.Source.Ref, Path: server.Source.Path,
		Runtime: server.Source.Runtime, Command: server.Command,
	}
}

func newPythonGitFixture(t *testing.T) (string, plumbing.Hash) {
	t.Helper()
	repository := t.TempDir()
	project := filepath.Join(repository, "services", "fixture")
	if err := os.MkdirAll(filepath.Join(project, "fixture"), 0755); err != nil {
		t.Fatalf("create Python fixture: %v", err)
	}
	writePythonFixture(t, project, "0.1.0")
	repo, err := git.PlainInit(repository, false)
	if err != nil {
		t.Fatalf("init Git fixture: %v", err)
	}
	return repository, commitPythonFixture(t, repo, "initial fixture")
}

func updatePythonGitFixture(t *testing.T, repository, version string) plumbing.Hash {
	t.Helper()
	writePythonFixture(t, filepath.Join(repository, "services", "fixture"), version)
	repo, err := git.PlainOpen(repository)
	if err != nil {
		t.Fatalf("open Git fixture: %v", err)
	}
	return commitPythonFixture(t, repo, "fixture "+version)
}

func writePythonFixture(t *testing.T, project, version string) {
	t.Helper()
	pyproject := fmt.Sprintf(`[build-system]
requires = ["setuptools>=70"]
build-backend = "setuptools.build_meta"

[project]
name = "gridctl-integration-mcp"
version = %q
requires-python = ">=3.10"

[project.scripts]
gridctl-integration-mcp = "fixture.server:main"
`, version)
	files := map[string]string{
		"pyproject.toml":      pyproject,
		"fixture/__init__.py": "",
		"fixture/server.py":   pythonFixtureModule,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(content), 0644); err != nil {
			t.Fatalf("write Python fixture %s: %v", name, err)
		}
	}
}

func commitPythonFixture(t *testing.T, repo *git.Repository, message string) plumbing.Hash {
	t.Helper()
	worktree, err := repo.Worktree()
	if err != nil {
		t.Fatalf("open fixture worktree: %v", err)
	}
	if err := worktree.AddWithOptions(&git.AddOptions{All: true}); err != nil {
		t.Fatalf("stage fixture: %v", err)
	}
	hash, err := worktree.Commit(message, &git.CommitOptions{Author: &object.Signature{
		Name: "gridctl integration", Email: "integration@gridctl.dev", When: time.Now(),
	}})
	if err != nil {
		t.Fatalf("commit fixture: %v", err)
	}
	return hash
}

func cleanupStack(t *testing.T, ctx context.Context, orchestrator *runtime.Orchestrator, name string) {
	t.Helper()
	_ = orchestrator.Down(ctx, name)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = orchestrator.Down(cleanupCtx, name)
	})
}

func onlyServer(t *testing.T, result *runtime.UpResult) runtime.MCPServerResult {
	t.Helper()
	if len(result.MCPServers) != 1 {
		t.Fatalf("MCP server results = %d, want 1", len(result.MCPServers))
	}
	return result.MCPServers[0]
}

func connectStdio(t *testing.T, ctx context.Context, orchestrator *runtime.Orchestrator, id runtime.WorkloadID) *mcp.StdioClient {
	t.Helper()
	client := mcp.NewStdioClient("python-integration", string(id), orchestrator.DockerClient())
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("initialize stdio client: %v", err)
	}
	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("refresh stdio tools: %v", err)
	}
	return client
}

func assertEchoCall(t *testing.T, ctx context.Context, client *mcp.StdioClient, message string) {
	t.Helper()
	if !hasTool(client.Tools(), "echo") {
		t.Fatalf("Git fixture tools = %v, want echo", toolNames(client.Tools()))
	}
	result, err := client.CallTool(ctx, "echo", map[string]any{"message": message})
	if err != nil {
		t.Fatalf("Git fixture echo tool call: %v", err)
	}
	if result.IsError || len(result.Content) != 1 || result.Content[0].Text != message {
		t.Fatalf("Git fixture echo result = %+v, want %q", result, message)
	}
}

func hasTool(tools []mcp.Tool, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func testPythonAutoscaler(t *testing.T, ctx context.Context, orchestrator *runtime.Orchestrator, image string) {
	t.Helper()
	const stackName = "inttest-python-autoscale"
	cleanupStack(t, ctx, orchestrator, stackName)
	if err := orchestrator.Runtime().EnsureNetwork(ctx, stackName+"-net", runtime.NetworkOptions{Driver: "bridge", Stack: stackName}); err != nil {
		t.Fatalf("ensure autoscaler network: %v", err)
	}

	gateway := mcp.NewGateway()
	gateway.SetDockerClient(orchestrator.DockerClient())
	t.Cleanup(gateway.Close)
	server := config.MCPServer{Name: "fixture", Transport: "stdio"}
	spawner := controller.NewContainerSpawner(controller.ContainerSpawnerOptions{
		Builder: gateway, Runtime: orchestrator.Runtime(), Stack: stackName, Server: server,
		Network: stackName + "-net", Image: image, Transport: "stdio", Ports: controller.NewAtomicPortAllocator(0),
	})
	template := mcp.MCPServerConfig{Name: server.Name, Transport: mcp.TransportStdio}
	policy := mcp.AutoscalePolicy{Min: 0, Max: 1, TargetInFlight: 1, IdleToZero: true, ScaleUpAfter: 30 * time.Second, ScaleDownAfter: time.Minute}
	if err := gateway.RegisterAutoscaler(ctx, template, mcp.ReplicaPolicyRoundRobin, spawner, policy); err != nil {
		t.Fatalf("register Python autoscaler: %v", err)
	}
	result, err := gateway.HandleToolsCall(ctx, mcp.ToolCallParams{
		Name: mcp.PrefixTool(server.Name, "echo"), Arguments: map[string]any{"message": "cold start"},
	})
	if err != nil {
		t.Fatalf("autoscaler cold-start tool call: %v", err)
	}
	if result.IsError || len(result.Content) != 1 || result.Content[0].Text != "cold start" {
		t.Fatalf("autoscaler tool result = %+v", result)
	}
	statuses, err := orchestrator.Status(ctx, stackName)
	if err != nil {
		t.Fatalf("autoscaler status: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("autoscaler workloads = %+v, want one", statuses)
	}
	status, err := orchestrator.Runtime().Status(ctx, statuses[0].ID)
	if err != nil {
		t.Fatalf("autoscaler workload status: %v", err)
	}
	if status.Image != image || status.HostPort != 0 {
		t.Errorf("autoscaler workload = %+v, want image %q and host port 0", status, image)
	}
}
