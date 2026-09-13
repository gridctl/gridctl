package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestExecution_ExportAndWholeServerReplacement(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.yaml")
	input := "name: fixture\nmcp-servers:\n- name: fixture\n  image: alpine\n  transport: stdio\n  env: {TOKEN: '${var:TOKEN}'}\n  execution:\n    mode: hardened\n    uid: 1000\n    gid: 1000\n    read_only: false\n    drop_capabilities: []\n    tmpfs: []\n    mounts: []\n"
	if err := os.WriteFile(parent, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	exported, _, err := ExportStack(t.Context(), parent)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(exported)
	if err != nil {
		t.Fatal(err)
	}
	var restored Stack
	if err := yaml.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.MCPServers[0].Execution.ReadOnly == nil || *restored.MCPServers[0].Execution.ReadOnly || restored.MCPServers[0].Execution.Mounts == nil || restored.MCPServers[0].Env["TOKEN"] != "${var:TOKEN}" {
		t.Fatal("export lost execution presence or secret reference")
	}
	child := filepath.Join(dir, "child.yaml")
	if err := os.WriteFile(child, []byte("name: child\nextends: parent.yaml\nmcp-servers:\n- name: fixture\n  image: alpine\n  transport: stdio\n"), 0600); err != nil {
		t.Fatal(err)
	}
	replaced, _, err := ExportStack(t.Context(), child)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.MCPServers[0].Execution != nil {
		t.Fatal("extends silently deep-merged execution")
	}
}

func TestExecution_ValidationReturnsDesiredNotEvidence(t *testing.T) {
	uid, gid := uint32(1000), uint32(1000)
	stack := &Stack{Name: "fixture", MCPServers: []MCPServer{{Name: "fixture", Image: "alpine", Transport: "stdio", Execution: &ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid}}}}
	stack.SetDefaults()
	result := ValidateWithIssues(stack)
	report := result.Execution["fixture"]
	if !result.Valid || report == nil || report.Eligible || report.Instance != "" || report.Outcome != "pending" {
		t.Fatalf("validation confused intent and evidence: %+v", result)
	}
	if len(report.Controls) < 10 {
		t.Fatal("review lacks resolved restrictions")
	}
}

func TestExecutionConfig_JSONStrict(t *testing.T) {
	for _, data := range []string{`{"mode":"local","surprise":true}`, `{"mode":"local","inherit":null}`, `{"mode":"hardened","mounts":[{"source":"volume","surprise":true}]}`} {
		var cfg ExecutionConfig
		if err := json.Unmarshal([]byte(data), &cfg); err == nil {
			t.Fatalf("accepted unknown/null field: %s", data)
		}
	}
}

func TestExecutionEqual_RevisionAndPresence(t *testing.T) {
	base := MCPServer{Command: []string{"/bin/cat"}}
	names := []string{}
	protected := base
	protected.Execution = &ExecutionConfig{Mode: "local", Inherit: &names}
	if ExecutionEqual(base, protected) || !ExecutionOnlyChange(base, protected) {
		t.Fatal("execution-only change not classified")
	}
	if MCPServerEqual(base, protected) {
		t.Fatal("execution ignored by canonical equality")
	}
	equivalent := protected
	equivalent.Execution = &ExecutionConfig{Mode: "local", Lookup: "absolute", Inherit: &names}
	if !MCPServerEqual(protected, equivalent) {
		t.Fatal("equivalent lookup default differs")
	}
	equivalent.Command = []string{"/bin/echo"}
	if ExecutionOnlyChange(base, equivalent) {
		t.Fatal("command change incorrectly preserves pins")
	}
}

func TestExecutionConfig_PresenceRoundTrip(t *testing.T) {
	for _, input := range []string{
		"mode: local\ninherit: []\n",
		"mode: hardened\nread_only: false\ndrop_capabilities: []\nmounts: []\n",
	} {
		var original ExecutionConfig
		if err := yaml.Unmarshal([]byte(input), &original); err != nil {
			t.Fatal(err)
		}
		out, err := yaml.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		var restored ExecutionConfig
		if err := yaml.Unmarshal(out, &restored); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(original, restored) {
			t.Fatalf("presence lost: %s", out)
		}
	}
}

func TestExecutionConfig_UnknownKeys(t *testing.T) {
	for _, input := range []string{"mode: local\ninhert: []", "mode: hardened\nmounts:\n- target: /data\n  surprise: true", "mode: hardened\ntmpfs:\n- target: /tmp\n  surprise: true"} {
		var cfg ExecutionConfig
		if err := yaml.Unmarshal([]byte(input), &cfg); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestResolveExecution(t *testing.T) {
	server := MCPServer{Name: "fixture", Image: "fixture", Transport: "stdio"}
	if contract, err := ResolveExecution(server); contract != nil || err != nil {
		t.Fatalf("omission changed behavior: %v %v", contract, err)
	}
	for _, tc := range []struct{ yaml, want string }{
		{"mode: wrong", "mode"},
		{"mode: hardened", "uid"},
		{"mode: hardened\nuid: 1\ngid: 1\nnetwork: internal", "network"},
		{"mode: hardened\nuid: 1\ngid: 1\nmemory_bytes: 0", "memory_bytes"},
		{"mode: hardened\nuid: 1\ngid: 1\ninherit: []", "inherit"},
		{"mode: local\ninherit: []", "local"},
	} {
		var cfg ExecutionConfig
		if err := yaml.Unmarshal([]byte(tc.yaml), &cfg); err != nil {
			t.Fatal(err)
		}
		server.Execution = &cfg
		if _, err := ResolveExecution(server); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: %v", tc.yaml, err)
		}
	}
	var cfg ExecutionConfig
	if err := yaml.Unmarshal([]byte("mode: hardened\nuid: 1000\ngid: 1000"), &cfg); err != nil {
		t.Fatal(err)
	}
	server.Execution = &cfg
	contract, err := ResolveExecution(server)
	if err != nil {
		t.Fatal(err)
	}
	if !contract.ReadOnly || !contract.NoNewPrivileges || contract.Network != "none" || contract.Revision == "" {
		t.Fatalf("missing restrictive defaults: %+v", contract)
	}
	server.Network = "connected"
	if _, err := ResolveExecution(server); err == nil {
		t.Fatal("network conflict accepted")
	}
}
