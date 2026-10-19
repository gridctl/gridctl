package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestClone_PreservesZeroPointersAndUnexportedFields(t *testing.T) {
	enabled := false
	pin := false
	uid := uint32(0)
	gid := 0
	inherit := []string{}
	var nilSlice []string
	stack := &Stack{
		Name: "demo",
		MCPServers: []MCPServer{{
			Name:       "parked",
			Enabled:    &enabled,
			PinSchemas: &pin,
			Execution: &ExecutionConfig{
				Mode:             "hardened",
				UID:              &uid,
				NoNewPrivileges:  &enabled,
				DropCapabilities: &inherit,
				Inherit:          &nilSlice,
			},
			Configs: []ConfigFile{{Target: "/etc/app", UID: &gid}},
		}},
		Resources: []Resource{{Name: "db", Enabled: &enabled}},
		Secrets: &Secrets{Sets: []SecretSetRef{{
			Name:          "shared",
			scopeDeclared: true,
		}}},
		overrideWarnings: []string{"parent-only"},
		emptySecretSites: []Consumer{{Kind: RefKindMCPServer, Name: "parked", Field: "env.TOKEN"}},
	}

	clone := stack.Clone()
	if clone == nil || clone == stack {
		t.Fatal("clone was not a distinct stack")
	}
	srv := clone.MCPServers[0]
	if srv.Enabled == nil || *srv.Enabled {
		t.Fatalf("enabled = %v", srv.Enabled)
	}
	if srv.PinSchemas == nil || *srv.PinSchemas {
		t.Fatalf("pin_schemas = %v", srv.PinSchemas)
	}
	if srv.Execution == nil || srv.Execution.UID == nil || *srv.Execution.UID != 0 {
		t.Fatalf("uid = %#v", srv.Execution)
	}
	if srv.Execution.NoNewPrivileges == nil || *srv.Execution.NoNewPrivileges {
		t.Fatalf("no_new_privileges = %v", srv.Execution.NoNewPrivileges)
	}
	if srv.Execution.DropCapabilities == nil || srv.Execution.Inherit == nil {
		t.Fatalf("presence-aware slices collapsed: %#v", srv.Execution)
	}
	if len(*srv.Execution.DropCapabilities) != 0 {
		t.Fatalf("drop_capabilities = %#v", *srv.Execution.DropCapabilities)
	}
	if srv.Configs[0].UID == nil || *srv.Configs[0].UID != 0 {
		t.Fatalf("configs uid = %v", srv.Configs[0].UID)
	}
	if clone.Resources[0].Enabled == nil || *clone.Resources[0].Enabled {
		t.Fatalf("resource enabled = %v", clone.Resources[0].Enabled)
	}
	if !clone.Secrets.Sets[0].scopeDeclared {
		t.Fatal("scopeDeclared was dropped")
	}
	if len(clone.overrideWarnings) != 1 || clone.overrideWarnings[0] != "parent-only" {
		t.Fatalf("overrideWarnings = %#v", clone.overrideWarnings)
	}
	if len(clone.emptySecretSites) != 1 {
		t.Fatalf("emptySecretSites = %#v", clone.emptySecretSites)
	}

	*srv.Enabled = true
	*srv.Execution.DropCapabilities = append(*srv.Execution.DropCapabilities, "ALL")
	clone.overrideWarnings[0] = "changed"
	if *stack.MCPServers[0].Enabled {
		t.Fatal("clone mutated the original enabled pointer")
	}
	if len(*stack.MCPServers[0].Execution.DropCapabilities) != 0 {
		t.Fatal("clone shared a presence-aware slice")
	}
	if stack.overrideWarnings[0] != "parent-only" {
		t.Fatal("clone shared overrideWarnings")
	}
}

func TestClone_EmptySlicePointerSurvivesYAMLRoundTripSource(t *testing.T) {
	body := []byte("version: \"1\"\nname: demo\nmcp-servers:\n  - name: parked\n    image: alpine:3\n    enabled: false\n    pin_schemas: false\n    execution:\n      mode: hardened\n      uid: 0\n      inherit: []\n    configs:\n      - target: /etc/app\n        content: x\n        uid: 0\n")
	stack, err := DecodeStack(body)
	if err != nil {
		t.Fatal(err)
	}
	clone := stack.Clone()
	srv := clone.MCPServers[0]
	if srv.Enabled == nil || *srv.Enabled || srv.PinSchemas == nil || *srv.PinSchemas {
		t.Fatalf("flags = enabled %v pin %v", srv.Enabled, srv.PinSchemas)
	}
	if srv.Execution == nil || srv.Execution.UID == nil || *srv.Execution.UID != 0 || srv.Execution.Inherit == nil {
		t.Fatalf("execution = %#v", srv.Execution)
	}
	if srv.Configs[0].UID == nil || *srv.Configs[0].UID != 0 {
		t.Fatalf("uid = %v", srv.Configs[0].UID)
	}
	encoded, err := yaml.Marshal(clone)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) == 0 {
		t.Fatal("marshal produced nothing")
	}
}
