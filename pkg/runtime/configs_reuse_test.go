package runtime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

func TestOrchestrator_Up_ReplacesChangedConfigs(t *testing.T) {
	rt := newSourceLifecycleRuntime()
	orch := NewOrchestrator(rt, &recordingSourceBuilder{})
	stack := configStack("alpha")
	if _, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000}); err != nil {
		t.Fatal(err)
	}
	if len(rt.started) != 1 || len(rt.started[0].Configs) != 1 || rt.started[0].Labels[LabelConfigsRevision] == "" {
		t.Fatalf("first start = %+v", rt.started)
	}
	firstID := rt.statuses["gridctl-demo-server"].ID
	if _, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000}); err != nil {
		t.Fatal(err)
	}
	if len(rt.removed) != 0 {
		t.Fatalf("unchanged config was replaced: removed=%v started=%d", rt.removed, len(rt.started))
	}
	stack.MCPServers[0].Configs[0].Content = "beta"
	if _, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000}); err != nil {
		t.Fatal(err)
	}
	if len(rt.removed) != 1 || rt.removed[0] != firstID {
		t.Fatalf("removed = %v, want %s", rt.removed, firstID)
	}
	if rt.started[len(rt.started)-1].Labels[LabelConfigsRevision] == rt.started[0].Labels[LabelConfigsRevision] {
		t.Fatal("replacement kept the old configs revision")
	}
}

func TestOrchestrator_Up_ConfigReadErrorBeforeEngineStart(t *testing.T) {
	rt := newSourceLifecycleRuntime()
	orch := NewOrchestrator(rt, &recordingSourceBuilder{})
	stack := configStack("alpha")
	stack.MCPServers[0].Configs[0].File = filepath.Join(t.TempDir(), "missing.yaml")
	stack.MCPServers[0].Configs[0].Content = ""
	_, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000})
	if err == nil || !strings.Contains(err.Error(), "server") || !strings.Contains(err.Error(), "config 0") {
		t.Fatalf("error = %v", err)
	}
	if len(rt.started) != 0 || len(rt.ensuredImages) != 0 {
		t.Fatalf("engine calls after revision failure: started=%d images=%v", len(rt.started), rt.ensuredImages)
	}
}

func TestOrchestrator_Up_ReusesEngineExpandedImage(t *testing.T) {
	rt := newSourceLifecycleRuntime()
	stack := configStack("alpha")
	revision, err := config.ConfigsRevision(context.Background(), stack.MCPServers[0].Configs)
	if err != nil {
		t.Fatal(err)
	}
	rt.statuses["gridctl-demo-server"] = &WorkloadStatus{
		ID:     "existing",
		State:  WorkloadStateRunning,
		Image:  "docker.io/library/alpine:latest",
		Labels: map[string]string{LabelConfigsRevision: revision},
	}
	orch := NewOrchestrator(rt, &recordingSourceBuilder{})
	if _, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000}); err != nil {
		t.Fatal(err)
	}
	if len(rt.removed) != 0 || len(rt.started) != 0 {
		t.Fatalf("expanded image replaced container: removed=%v started=%d", rt.removed, len(rt.started))
	}

	rt.statuses["gridctl-demo-server"].Image = "docker.io/library/busybox:latest"
	if _, err := orch.Up(context.Background(), stack, UpOptions{BasePort: 9000}); err != nil {
		t.Fatal(err)
	}
	if len(rt.removed) != 1 || rt.removed[0] != "existing" {
		t.Fatalf("removed = %v, want existing", rt.removed)
	}
}

func TestOrchestrator_ReuseStartOmitsConfigs(t *testing.T) {
	rt := newSourceLifecycleRuntime()
	revision, err := config.ConfigsRevision(context.Background(), []config.ConfigFile{{Target: "/etc/a.txt", Content: "alpha", Mode: "0444"}})
	if err != nil {
		t.Fatal(err)
	}
	rt.statuses["gridctl-demo-server"] = &WorkloadStatus{
		ID:     "existing",
		State:  WorkloadStateStopped,
		Image:  "alpine",
		Labels: map[string]string{LabelConfigsRevision: revision},
	}
	orch := NewOrchestrator(rt, &recordingSourceBuilder{})
	if _, err := orch.Up(context.Background(), configStack("alpha"), UpOptions{BasePort: 9000}); err != nil {
		t.Fatal(err)
	}
	if len(rt.started) != 1 {
		t.Fatalf("start calls = %d, want the minimal reuse start", len(rt.started))
	}
	got := rt.started[0]
	if got.Name != "server" || got.Stack != "demo" || len(got.Configs) != 0 || got.Labels != nil || got.Image != "" {
		t.Fatalf("reuse start = %+v", got)
	}
}

func configStack(content string) *config.Stack {
	return &config.Stack{
		Name:    "demo",
		Network: config.Network{Name: "demo-net", Driver: "bridge"},
		MCPServers: []config.MCPServer{{
			Name:      "server",
			Image:     "alpine",
			Transport: "stdio",
			Configs:   []config.ConfigFile{{Target: "/etc/a.txt", Content: content, Mode: "0444"}},
		}},
	}
}
