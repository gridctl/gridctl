//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/controller"
	"github.com/gridctl/gridctl/pkg/dockerclient"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/reload"
	"github.com/gridctl/gridctl/pkg/runtime"
	_ "github.com/gridctl/gridctl/pkg/runtime/docker"
)

type configVault map[string]string

func (v configVault) Get(key string) (string, bool) {
	value, ok := v[key]
	return value, ok
}

func TestStackConfigs_MaterializeOwnershipAndReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	rt, err := runtime.New()
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	defer rt.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	dir := t.TempDir()
	stackPath := dir + "/stack.yaml"
	if err := os.WriteFile(dir+"/extra.yaml", []byte("from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConfigStack(t, stackPath, stackName, netName, "tenant=${var:MIMIR_TENANT}")
	stack, err := config.LoadStack(stackPath, config.WithVault(configVault{"MIMIR_TENANT": "acme"}))
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Down(context.Background(), stackName)

	first, err := rt.Up(ctx, stack, runtime.UpOptions{BasePort: 22100})
	if err != nil {
		t.Fatalf("Up: %v", err)
	}
	id := string(first.MCPServers[0].WorkloadID)
	parent := execOutput(t, ctx, rt.DockerClient(), id, []string{"stat", "-c", "%u %g %a", "/etc"})
	inline := execOutput(t, ctx, rt.DockerClient(), id, []string{"stat", "-c", "%u %g %a", "/etc/gridctl-cfg/http.yaml"})
	fileStat := execOutput(t, ctx, rt.DockerClient(), id, []string{"stat", "-c", "%u %g %a", "/gridctl-missing/nested/extra.yaml"})
	inlineBody := execOutput(t, ctx, rt.DockerClient(), id, []string{"cat", "/etc/gridctl-cfg/http.yaml"})
	fileBody := execOutput(t, ctx, rt.DockerClient(), id, []string{"cat", "/gridctl-missing/nested/extra.yaml"})
	if !strings.Contains(inlineBody, "tenant=acme") {
		t.Fatalf("inline content = %q", inlineBody)
	}
	if strings.TrimSpace(fileBody) != "from-file" {
		t.Fatalf("file content = %q", fileBody)
	}
	if got := strings.TrimSpace(inline); got != "0 0 444" && got != "0 0 0444" {
		t.Fatalf("inline ownership = %q", inline)
	}
	if got := strings.TrimSpace(fileStat); got != "65534 65534 440" && got != "65534 65534 0440" {
		t.Fatalf("file ownership = %q", fileStat)
	}

	baseline, err := rt.Runtime().Start(ctx, runtime.WorkloadConfig{
		Name: "baseline", Stack: stackName, Type: runtime.WorkloadTypeMCPServer,
		Image: "alpine:latest", Command: sleepCmd, NetworkName: netName, Transport: "stdio",
		Labels: map[string]string{"gridctl.managed": "true", "gridctl.stack": stackName, "gridctl.mcp-server": "baseline"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Runtime().Remove(context.Background(), baseline.ID)
	baseParent := execOutput(t, ctx, rt.DockerClient(), string(baseline.ID), []string{"stat", "-c", "%u %g %a", "/etc"})
	if strings.TrimSpace(parent) != strings.TrimSpace(baseParent) {
		t.Fatalf("/etc = %q, baseline %q", parent, baseParent)
	}
	inspected, err := rt.DockerClient().ContainerInspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if inspected.Config.Labels["gridctl.configs-revision"] == "" {
		t.Fatal("missing configs revision label")
	}

	second, err := rt.Up(ctx, stack, runtime.UpOptions{BasePort: 22100})
	if err != nil {
		t.Fatal(err)
	}
	if string(second.MCPServers[0].WorkloadID) != id {
		t.Fatalf("unchanged apply recreated %s as %s (image %q)", id, second.MCPServers[0].WorkloadID, inspected.Config.Image)
	}

	writeConfigStack(t, stackPath, stackName, netName, "tenant=beta")
	changed, err := config.LoadStack(stackPath, config.WithVault(configVault{"MIMIR_TENANT": "acme"}))
	if err != nil {
		t.Fatal(err)
	}
	third, err := rt.Up(ctx, changed, runtime.UpOptions{BasePort: 22100})
	if err != nil {
		t.Fatal(err)
	}
	newID := string(third.MCPServers[0].WorkloadID)
	if newID == id {
		t.Fatal("changed config reused the old container")
	}
	reloaded := execOutput(t, ctx, rt.DockerClient(), newID, []string{"cat", "/etc/gridctl-cfg/http.yaml"})
	if !strings.Contains(reloaded, "tenant=beta") {
		t.Fatalf("recreated content = %q", reloaded)
	}
	again, err := rt.DockerClient().ContainerInspect(ctx, newID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Config.Labels["gridctl.configs-revision"] == inspected.Config.Labels["gridctl.configs-revision"] {
		t.Fatal("revision label did not change")
	}
}

func TestStackConfigs_HotReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	rt, err := runtime.New()
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	stackName := "inttest-" + sanitizeName(t.Name())
	dir := t.TempDir()
	stackPath := dir + "/stack.yaml"
	if err := os.WriteFile(dir+"/extra.yaml", []byte("from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeConfigStack(t, stackPath, stackName, stackName+"-net", "before")
	stack, err := config.LoadStack(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Down(context.Background(), stackName)
	up, err := rt.Up(ctx, stack, runtime.UpOptions{BasePort: 22200})
	if err != nil {
		t.Fatal(err)
	}
	handler := reload.NewHandler(stackPath, stack, mcp.NewGateway(), rt, 0, 22210, nil, nil)
	writeConfigStack(t, stackPath, stackName, stackName+"-net", "after")
	result, err := handler.Reload(ctx)
	if err != nil || !result.Success {
		t.Fatalf("Reload err=%v result=%+v", err, result)
	}
	if len(result.Modified) != 1 {
		t.Fatalf("modified = %v", result.Modified)
	}
	statuses, err := rt.Status(ctx, stackName)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for _, status := range statuses {
		if status.Labels["gridctl.mcp-server"] == "prometheus" {
			id = string(status.ID)
		}
	}
	if id == "" || id == string(up.MCPServers[0].WorkloadID) {
		t.Fatalf("reload did not replace %s, statuses=%+v", up.MCPServers[0].WorkloadID, statuses)
	}
	body := execOutput(t, ctx, rt.DockerClient(), id, []string{"cat", "/etc/gridctl-cfg/http.yaml"})
	if !strings.Contains(body, "after") {
		t.Fatalf("reloaded content = %q", body)
	}
}

func TestStackConfigs_AutoscaleSpawn(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	rt, err := runtime.New()
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	stackName := "inttest-" + sanitizeName(t.Name())
	netName := stackName + "-net"
	defer rt.Down(context.Background(), stackName)
	if err := rt.Runtime().EnsureNetwork(ctx, netName, runtime.NetworkOptions{Driver: "bridge", Stack: stackName}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Runtime().EnsureImage(ctx, "alpine:latest"); err != nil {
		t.Fatal(err)
	}
	server := config.MCPServer{
		Name: "prometheus", Image: "alpine:latest", Transport: "stdio", Command: sleepCmd,
		Configs: []config.ConfigFile{{Target: "/etc/gridctl-cfg/http.yaml", Content: "spawned\n", Mode: "0444"}},
	}
	spawner := controller.NewContainerSpawner(controller.ContainerSpawnerOptions{
		Builder: configSpawnBuilder{}, Runtime: rt.Runtime(), Stack: stackName, Server: server,
		Network: netName, Image: server.Image, Transport: "stdio", Ports: controller.NewAtomicPortAllocator(0),
	})
	client, err := spawner.Spawn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	statuses, err := rt.Status(ctx, stackName)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 {
		t.Fatalf("spawned workloads = %d", len(statuses))
	}
	body := execOutput(t, ctx, rt.DockerClient(), string(statuses[0].ID), []string{"cat", "/etc/gridctl-cfg/http.yaml"})
	if strings.TrimSpace(body) != "spawned" {
		t.Fatalf("spawned content = %q", body)
	}
	set := mcp.NewReplicaSet(server.Name, mcp.ReplicaPolicyRoundRobin, []mcp.AgentClient{client})
	if err := spawner.Reap(ctx, set.Replicas()[0]); err != nil {
		t.Fatal(err)
	}
}

func TestStackConfigs_MissingFileLeavesNoContainer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	rt, err := runtime.New()
	if err != nil {
		t.Skipf("container runtime not available: %v", err)
	}
	defer rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	stackName := "inttest-" + sanitizeName(t.Name())
	dir := t.TempDir()
	stackPath := dir + "/stack.yaml"
	body := "version: \"1\"\nname: " + stackName + "\nnetwork:\n  name: " + stackName + "-net\n  driver: bridge\nmcp-servers:\n  - name: prometheus\n    image: alpine:latest\n    transport: stdio\n    command: [\"sleep\", \"30\"]\n    configs:\n      - target: /etc/gridctl-cfg/http.yaml\n        file: ./missing.yaml\n"
	if err := os.WriteFile(stackPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := config.LoadStack(stackPath)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Down(context.Background(), stackName)
	_, err = rt.Up(ctx, stack, runtime.UpOptions{BasePort: 22300})
	if err == nil || !strings.Contains(err.Error(), "prometheus") || !strings.Contains(err.Error(), "config 0") {
		t.Fatalf("Up error = %v", err)
	}
	statuses, statusErr := rt.Status(ctx, stackName)
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if len(statuses) != 0 {
		t.Fatalf("containers left behind: %+v", statuses)
	}
}

func TestStackConfigs_HardenedRejected(t *testing.T) {
	uid := 65534
	err := config.Validate(&config.Stack{
		Name: "demo",
		MCPServers: []config.MCPServer{{
			Name: "prometheus", Image: "alpine", Transport: "stdio",
			Execution: &config.ExecutionConfig{Mode: "hardened", UID: uint32Ptr(uint32(uid)), GID: uint32Ptr(uint32(uid))},
			Configs:   []config.ConfigFile{{Target: "/etc/app.yaml", Content: "nope"}},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "not supported with execution.mode: hardened") {
		t.Fatalf("Validate error = %v", err)
	}
}

func uint32Ptr(v uint32) *uint32 { return &v }

type configSpawnBuilder struct{}

func (configSpawnBuilder) BuildAgentClient(context.Context, mcp.MCPServerConfig) (mcp.AgentClient, error) {
	return &configSpawnClient{name: "prometheus"}, nil
}

type configSpawnClient struct{ name string }

func (c *configSpawnClient) Name() string                     { return c.name }
func (c *configSpawnClient) Initialize(context.Context) error { return nil }
func (c *configSpawnClient) RefreshTools(context.Context) error {
	return nil
}
func (c *configSpawnClient) Tools() []mcp.Tool { return nil }
func (c *configSpawnClient) CallTool(context.Context, string, map[string]any) (*mcp.ToolCallResult, error) {
	return &mcp.ToolCallResult{}, nil
}
func (c *configSpawnClient) IsInitialized() bool        { return true }
func (c *configSpawnClient) ServerInfo() mcp.ServerInfo { return mcp.ServerInfo{Name: c.name} }

func writeConfigStack(t *testing.T, path, name, network, content string) {
	t.Helper()
	body := fmt.Sprintf("version: \"1\"\nname: %s\nnetwork:\n  name: %s\n  driver: bridge\nmcp-servers:\n  - name: prometheus\n    image: alpine:latest\n    transport: stdio\n    command: [\"sh\", \"-c\", \"while true; do sleep 1; done\"]\n    configs:\n      - target: /etc/gridctl-cfg/http.yaml\n        content: %q\n      - target: /gridctl-missing/nested/extra.yaml\n        file: ./extra.yaml\n        mode: \"0440\"\n        uid: 65534\n        gid: 65534\n", name, network, content)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func execOutput(t *testing.T, ctx context.Context, cli dockerclient.DockerClient, id string, cmd []string) string {
	t.Helper()
	type execAPI interface {
		ContainerExecCreate(context.Context, string, container.ExecOptions) (container.ExecCreateResponse, error)
		ContainerExecAttach(context.Context, string, container.ExecStartOptions) (types.HijackedResponse, error)
	}
	api, ok := cli.(execAPI)
	if !ok {
		t.Fatal("runtime client does not support exec")
	}
	created, err := api.ContainerExecCreate(ctx, id, container.ExecOptions{Cmd: cmd, AttachStdout: true, AttachStderr: true})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := api.ContainerExecAttach(ctx, created.ID, container.ExecStartOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Close()
	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, resp.Reader); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() == 0 && stderr.Len() > 0 {
		t.Fatalf("exec %v stderr: %s", cmd, stderr.String())
	}
	return stdout.String()
}
