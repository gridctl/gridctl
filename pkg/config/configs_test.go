package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configServer(configs ...ConfigFile) MCPServer {
	return MCPServer{Name: "server", Image: "alpine", Transport: "stdio", Configs: configs}
}

func TestValidate_Configs(t *testing.T) {
	uid := 65534
	badUID := -1
	hugeUID := 1 << 31
	tests := []struct {
		name    string
		server  MCPServer
		wantErr string
	}{
		{name: "inline content", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: "a: 1"})},
		{name: "file source", server: configServer(ConfigFile{Target: "/etc/app.yaml", File: "./app.yaml", Mode: "0440", UID: &uid, GID: &uid})},
		{name: "three digit mode", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: "a", Mode: "644"})},
		{name: "four digit mode", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: "a", Mode: "0640"})},
		{name: "go octal prefix", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: "a", Mode: "0o644"}), wantErr: "0o prefix is not accepted"},
		{name: "missing target", server: configServer(ConfigFile{Content: "a"}), wantErr: "configs[0].target: is required"},
		{name: "relative target", server: configServer(ConfigFile{Target: "etc/app.yaml", Content: "a"}), wantErr: "must be absolute"},
		{name: "unclean target", server: configServer(ConfigFile{Target: "/etc/../app.yaml", Content: "a"}), wantErr: "traversal"},
		{name: "root target", server: configServer(ConfigFile{Target: "/", Content: "a"}), wantErr: "must not be /"},
		{name: "proc target", server: configServer(ConfigFile{Target: "/proc/self/mem", Content: "a"}), wantErr: "must not be under /proc"},
		{name: "sys target", server: configServer(ConfigFile{Target: "/sys/class", Content: "a"}), wantErr: "must not be under /proc, /sys, or /dev"},
		{name: "dev target", server: configServer(ConfigFile{Target: "/dev/null", Content: "a"}), wantErr: "must not be under /proc, /sys, or /dev"},
		{name: "proc prefix is allowed", server: configServer(ConfigFile{Target: "/procfs/app.yaml", Content: "a"})},
		{name: "neither source", server: configServer(ConfigFile{Target: "/etc/app.yaml"}), wantErr: "exactly one of file or content"},
		{name: "both sources", server: configServer(ConfigFile{Target: "/etc/app.yaml", File: "./a", Content: "a"}), wantErr: "exactly one of file or content"},
		{name: "bad uid", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: "a", UID: &badUID}), wantErr: "uid"},
		{name: "uid above int31", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: "a", UID: &hugeUID}), wantErr: "uid"},
		{name: "duplicate target", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: "a"}, ConfigFile{Target: "/etc/app.yaml", Content: "b"}), wantErr: "duplicate target"},
		{name: "volume collision", server: MCPServer{Name: "server", Image: "alpine", Transport: "stdio", Volumes: []string{"/host:/etc/app.yaml:ro"}, Configs: []ConfigFile{{Target: "/etc/app.yaml", Content: "a"}}}, wantErr: "conflicts with a volume"},
		{name: "content too long", server: configServer(ConfigFile{Target: "/etc/app.yaml", Content: strings.Repeat("a", maxConfigBytes+1)}), wantErr: "at most 1 MiB"},
		{name: "not a container", server: MCPServer{Name: "server", Command: []string{"server"}, Configs: []ConfigFile{{Target: "/etc/app.yaml", Content: "a"}}}, wantErr: "only valid for container-based servers"},
		{name: "hardened", server: MCPServer{Name: "server", Image: "alpine", Transport: "stdio", Execution: &ExecutionConfig{Mode: "hardened"}, Configs: []ConfigFile{{Target: "/etc/app.yaml", Content: "a"}}}, wantErr: "not supported with execution.mode: hardened"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(&Stack{Name: "test", Network: Network{Name: "test-net"}, MCPServers: []MCPServer{tt.server}})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_ConfigsEntryCap(t *testing.T) {
	configs := make([]ConfigFile, maxConfigEntries+1)
	for i := range configs {
		configs[i] = ConfigFile{Target: "/etc/app-" + strings.Repeat("x", 0) + string(rune('a'+i%26)) + ".yaml", Content: "a"}
	}
	// Distinct absolute targets.
	for i := range configs {
		configs[i].Target = "/etc/cfg/" + strings.Repeat("n", i+1)
	}
	err := Validate(&Stack{Name: "test", Network: Network{Name: "test-net"}, MCPServers: []MCPServer{configServer(configs...)}})
	if err == nil || !strings.Contains(err.Error(), "at most 64") {
		t.Fatalf("Validate error = %v, want entry cap", err)
	}
}

func TestValidate_ResourceVolumes(t *testing.T) {
	err := Validate(&Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Resources: []Resource{{
			Name:    "db",
			Image:   "postgres",
			Volumes: []string{"data"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "resources[0].volumes[0]") {
		t.Fatalf("Validate error = %v, want resource volume field", err)
	}
	ok := Validate(&Stack{
		Name:    "test",
		Network: Network{Name: "test-net"},
		Resources: []Resource{{
			Name:    "db",
			Image:   "postgres",
			Volumes: []string{"data:/var/lib/postgres"},
		}},
	})
	if ok != nil {
		t.Fatalf("named resource volume: %v", ok)
	}
}

func TestSetDefaults_ConfigMode(t *testing.T) {
	stack := &Stack{MCPServers: []MCPServer{{Configs: []ConfigFile{{Target: "/etc/a", Content: "a"}, {Target: "/etc/b", Content: "b", Mode: "0640"}}}}}
	stack.SetDefaults()
	if stack.MCPServers[0].Configs[0].Mode != "0444" || stack.MCPServers[0].Configs[1].Mode != "0640" {
		t.Fatalf("modes = %#v", stack.MCPServers[0].Configs)
	}
}

func TestLoadStack_ConfigModeAndRelativeVolumes(t *testing.T) {
	dir := t.TempDir()
	parentDir := filepath.Join(dir, "parent")
	childDir := filepath.Join(dir, "child")
	if err := os.Mkdir(parentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(childDir, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := []byte("name: parent\nmcp-servers:\n  - name: inherited\n    image: alpine\n    transport: stdio\n    volumes:\n      - ./data:/data:ro\n")
	if err := os.WriteFile(filepath.Join(parentDir, "parent.yaml"), parent, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", "./x")
	t.Setenv("PROJECT_DIR", "/abs/project")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`version: "1"
name: demo
extends: ../parent/parent.yaml
mcp-servers:
  - name: srv
    image: alpine
    transport: stdio
    volumes:
      - ./data:/data:ro
      - ../data:/other:ro
      - ~/data:/home-data:ro
      - /abs:/abs:ro
      - named:/named
      - ${PROJECT_DIR}/x:/from-abs
    configs:
      - target: /etc/a.yaml
        content: hello
        mode: 0444
      - target: /etc/b.yaml
        content: hello
        mode: 644
      - target: /etc/c.yaml
        content: hello
        mode: "0440"
      - target: /etc/d.yaml
        file: ./extra.yaml
resources:
  - name: db
    image: postgres
    volumes:
      - ${DATA_DIR}:/var/lib/postgres
`)
	path := filepath.Join(childDir, "stack.yaml")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	stack, err := LoadStack(path)
	if err != nil {
		t.Fatal(err)
	}
	var child MCPServer
	for _, srv := range stack.MCPServers {
		if srv.Name == "srv" {
			child = srv
		}
	}
	got := child.Volumes
	want := []string{
		filepath.Join(childDir, "data") + ":/data:ro",
		filepath.Join(childDir, "..", "data") + ":/other:ro",
		filepath.Join(home, "data") + ":/home-data:ro",
		"/abs:/abs:ro",
		"named:/named",
		"/abs/project/x:/from-abs",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("volumes =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	modes := []string{child.Configs[0].Mode, child.Configs[1].Mode, child.Configs[2].Mode}
	if modes[0] != "0444" || modes[1] != "644" || modes[2] != "0440" {
		t.Fatalf("modes = %v", modes)
	}
	if child.Configs[3].File != filepath.Join(childDir, "extra.yaml") {
		t.Fatalf("file = %q", child.Configs[3].File)
	}
	if stack.Resources[0].Volumes[0] != filepath.Join(childDir, "x")+":/var/lib/postgres" {
		t.Fatalf("resource volume = %q", stack.Resources[0].Volumes[0])
	}
	var inherited MCPServer
	for _, srv := range stack.MCPServers {
		if srv.Name == "inherited" {
			inherited = srv
		}
	}
	if inherited.Volumes[0] != filepath.Join(parentDir, "data")+":/data:ro" {
		t.Fatalf("inherited volume = %q", inherited.Volumes)
	}
}

func TestValidateStackFile_ConfigsDoNotReadFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stack.yaml")
	body := []byte("name: demo\nmcp-servers:\n  - name: srv\n    image: alpine\n    transport: stdio\n    configs:\n      - target: /etc/app.yaml\n        file: ./missing.yaml\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	stack, result, err := ValidateStackFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ErrorCount != 0 {
		t.Fatalf("errors = %+v", result.Issues)
	}
	if stack.MCPServers[0].Configs[0].File != "./missing.yaml" {
		t.Fatalf("validate resolved file path: %q", stack.MCPServers[0].Configs[0].File)
	}
}

func TestConfigsRevision_DeterministicAndReadsFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "extra.yaml")
	if err := os.WriteFile(file, []byte("from-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	uid := 65534
	configs := []ConfigFile{
		{Target: "/etc/b.yaml", Content: "beta", Mode: "0440", UID: &uid, GID: &uid},
		{Target: "/etc/a.yaml", File: file},
	}
	first, err := ConfigsRevision(context.Background(), configs)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Fatalf("revision length = %d", len(first))
	}
	reversed := []ConfigFile{configs[1], configs[0]}
	second, err := ConfigsRevision(context.Background(), reversed)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("order changed revision: %s vs %s", first, second)
	}
	configs[0].Content = "gamma"
	third, err := ConfigsRevision(context.Background(), configs)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("content change did not change revision")
	}
	empty, err := ConfigsRevision(context.Background(), nil)
	if err != nil || empty != "" {
		t.Fatalf("empty revision = %q, %v", empty, err)
	}
	_, err = ConfigsRevision(context.Background(), []ConfigFile{{Target: "/etc/missing", File: filepath.Join(dir, "nope")}})
	if err == nil || !strings.Contains(err.Error(), "config 0") {
		t.Fatalf("missing file error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ConfigsRevision(ctx, configs)
	if err == nil {
		t.Fatal("expected canceled context")
	}
}

func TestReferencedConfigFiles(t *testing.T) {
	dir := t.TempDir()
	stack := &Stack{MCPServers: []MCPServer{{
		Configs: []ConfigFile{
			{File: "./extra.yaml"},
			{File: "/abs/extra.yaml"},
			{Content: "inline"},
		},
	}}}
	got, err := ReferencedConfigFiles(stack, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != filepath.Join(dir, "extra.yaml") || got[1] != "/abs/extra.yaml" {
		t.Fatalf("paths = %#v", got)
	}
	for _, file := range []string{"~/secret", "${VAR}/secret"} {
		_, err := ReferencedConfigFiles(&Stack{MCPServers: []MCPServer{{Configs: []ConfigFile{{File: file}}}}}, dir)
		if err == nil || !strings.Contains(err.Error(), "cannot be anchored without expansion") {
			t.Fatalf("file %q error = %v", file, err)
		}
	}
}

func TestMCPServerEqual_EmptyConfigs(t *testing.T) {
	a := MCPServer{Name: "s", Image: "alpine", Transport: "stdio"}
	b := a
	b.Configs = []ConfigFile{}
	if !MCPServerEqual(a, b) {
		t.Fatal("empty configs should compare equal to omission")
	}
	b.Configs = []ConfigFile{{Target: "/etc/a", Content: "a", Mode: "0444"}}
	if MCPServerEqual(a, b) {
		t.Fatal("configs change compared equal")
	}
}

func TestComputePlan_ConfigsChanged(t *testing.T) {
	current := &Stack{Name: "test", Network: Network{Name: "test-net"}, MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Configs: []ConfigFile{{Target: "/etc/a", Content: "old", Mode: "0444"}}}}}
	proposed := &Stack{Name: "test", Network: Network{Name: "test-net"}, MCPServers: []MCPServer{{Name: "s1", Image: "alpine", Configs: []ConfigFile{{Target: "/etc/a", Content: "new", Mode: "0444"}}}}}
	diff := ComputePlan(proposed, current)
	if !diff.HasChanges {
		t.Fatal("expected changes")
	}
	found := false
	for _, item := range diff.Items {
		for _, detail := range item.Details {
			if detail == "configs changed" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("details = %#v", diff.Items)
	}
}
