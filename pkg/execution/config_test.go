package execution

import (
	"encoding/json"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestResolveExecution_Contracts(t *testing.T) {
	for _, tc := range []struct {
		name, block          string
		local, external, bad bool
	}{
		{"container", "mode: hardened\nuid: 1000\ngid: 1000", false, false, false},
		{"local", "mode: local\ninherit: []", true, false, false},
		{"lookup exception", "mode: local\nlookup: ambient_path\ninherit: [PATH, HOME, PATH]", true, false, false},
		{"container exceptions", "mode: hardened\nuid: 1\ngid: 1\nnetwork: connected\nno_new_privileges: false\nread_only: false\ndrop_capabilities: []\ntmpfs: []\nmounts: []", false, false, false},
		{"data volume", "mode: hardened\nuid: 1\ngid: 1\nmounts: [{source: fixture-data, target: /data, read_only: false}]", false, false, false},
		{"numeric bounds", "mode: hardened\nuid: 1\ngid: 1\nmemory_bytes: 67108864\ncpu_millis: 500\npids: 16\ntmpfs: [{target: /tmp, size_bytes: 1048576}]\ndrop_capabilities: [cap_net_raw, CHOWN]", false, false, false},
		{"root", "mode: hardened\nuid: 0\ngid: 1", false, false, true},
		{"root group", "mode: hardened\nuid: 1\ngid: 0", false, false, true},
		{"bad mode", "mode: arbitrary", true, false, true},
		{"SSH", "mode: local", false, true, true},
		{"local container control", "mode: local\nread_only: true", true, false, true},
		{"local wrong transport", "mode: local", false, false, true},
		{"container wrong transport", "mode: hardened\nuid: 1\ngid: 1", true, false, true},
		{"container inheritance", "mode: hardened\nuid: 1\ngid: 1\ninherit: []", false, false, true},
		{"bad name", "mode: local\ninherit: ['NAME=value']", true, false, true},
		{"bad lookup", "mode: local\nlookup: child_path", true, false, true},
		{"memory zero", "mode: hardened\nuid: 1\ngid: 1\nmemory_bytes: 0", false, false, true},
		{"cpu zero", "mode: hardened\nuid: 1\ngid: 1\ncpu_millis: 0", false, false, true},
		{"pids zero", "mode: hardened\nuid: 1\ngid: 1\npids: 0", false, false, true},
		{"cap invalid", "mode: hardened\nuid: 1\ngid: 1\ndrop_capabilities: [INVALID]", false, false, true},
		{"seccomp unconfined", "mode: hardened\nuid: 1\ngid: 1\nseccomp: unconfined", false, false, true},
		{"network invalid", "mode: hardened\nuid: 1\ngid: 1\nnetwork: internal", false, false, true},
		{"host socket", "mode: hardened\nuid: 1\ngid: 1\nmounts: [{source: /var/run/docker.sock, target: /data}]", false, false, true},
		{"scratch code", "mode: hardened\nuid: 1\ngid: 1\ntmpfs: [{target: /app, size_bytes: 1}]", false, false, true},
		{"mount code", "mode: hardened\nuid: 1\ngid: 1\nmounts: [{source: fixture, target: /app}]", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg ExecutionConfig
			if err := yaml.Unmarshal([]byte(tc.block), &cfg); err != nil {
				t.Fatal(err)
			}
			contract, err := ResolveExecution(Server{Execution: &cfg, Local: tc.local, External: tc.external, Transport: "stdio", Command: []string{"/bin/cat"}})
			if (err != nil) != tc.bad {
				t.Fatalf("unexpected resolution: %v", err)
			}
			if err == nil {
				if len(contract.Revision) != 64 {
					t.Fatal("missing revision")
				}
				report := RequestedReport(contract)
				if report.Eligible || report.Outcome != "pending" {
					t.Fatal("desired intent became runtime evidence")
				}
				encoded, err := json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), "fixture-data") {
					t.Fatal("report exposed volume source")
				}
			}
		})
	}
	if contract, err := ResolveExecution(Server{}); err != nil || contract != nil || RequestedReport(contract) != nil {
		t.Fatal("omission changed compatibility behavior")
	}
}

func TestExecutionConfig_Decoding(t *testing.T) {
	for _, input := range []string{"mode: local\nunknown: secret-value", "mode: local\ninherit: null", "mode: local\nmode: hardened", "mode: hardened\nuid: secret-value", "[]", "mode: hardened\nmounts: [{source: fixture, target: /data, unknown: secret-value}]"} {
		var cfg ExecutionConfig
		err := yaml.Unmarshal([]byte(input), &cfg)
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("unsafe decoding result: %v", err)
		}
	}
	for _, input := range []string{`{"mode":"local","unknown":"secret-value"}`, `{"mode":"local","inherit":null}`, `{"uid":"secret-value"}`} {
		var cfg ExecutionConfig
		err := json.Unmarshal([]byte(input), &cfg)
		if err == nil || strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("unsafe JSON decoding result: %v", err)
		}
	}
	var cfg ExecutionConfig
	if err := json.Unmarshal([]byte(`{"mode":"local","inherit":[]}`), &cfg); err != nil || cfg.Inherit == nil {
		t.Fatal("empty inheritance lost")
	}
}
