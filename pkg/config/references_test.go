package config

import (
	"sort"
	"testing"
)

func TestExpandStringRefs_StoreRefsCaptured(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"canonical var ref", "${var:TOKEN}", []string{"TOKEN"}},
		{"deprecated vault ref", "${vault:OLD}", []string{"OLD"}},
		{"bare braced env is not a store ref", "${PLAIN}", nil},
		{"bare dollar env is not a store ref", "$PLAIN", nil},
		{"multiple refs in order", "${var:A}-${var:B}", []string{"A", "B"}},
		{"ref recorded even with default operator", "${var:KEY:-fallback}", []string{"KEY"}},
		{"store ref mixed with env ref", "${HOST}:${var:PORT}", []string{"PORT"}},
		{"no refs", "literal-value", nil},
	}

	// Empty resolver: nothing resolves, proving refs are captured independent
	// of resolution.
	resolve := func(string) (string, bool) { return "", false }

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, refs, _, _ := ExpandStringRefs(tt.input, resolve)
			if !equalStrings(refs, tt.want) {
				t.Errorf("storeRefs = %v, want %v", refs, tt.want)
			}
		})
	}
}

// TestExpandStackVars_ReferenceIndexParity is the lockstep guard: it places the
// same ${var:PROBE} reference in every expandable field and asserts the index
// records a consumer for each. If a field is expanded but not indexed (the
// cardinal under-counting sin), or labeled differently than expected, this
// fails. Add a newly expandable field to expandStackVars and you must add it
// here too.
func TestExpandStackVars_ReferenceIndexParity(t *testing.T) {
	const probe = "${var:PROBE}"
	stack := &Stack{
		Name: probe,
		Gateway: &GatewayConfig{
			AllowedOrigins: []string{probe},
			Auth:           &AuthConfig{Token: probe},
		},
		Network:  Network{Name: probe},
		Networks: []Network{{Name: probe}},
		MCPServers: []MCPServer{{
			Name:    probe,
			Image:   probe,
			URL:     probe,
			Network: probe,
			Command: []string{probe},
			Source: &Source{
				URL:  probe,
				Path: probe,
				Ref:  probe,
				Auth: &SourceAuth{
					CredentialRef: "${var:PROBE_CRED}", // intentionally NOT expanded/indexed
					SSHKeyPath:    probe,
					SSHUser:       probe,
				},
			},
			Env:       map[string]string{"E1": probe},
			BuildArgs: map[string]string{"B1": probe},
			SSH: &SSHConfig{
				Host:           probe,
				User:           probe,
				IdentityFile:   probe,
				KnownHostsFile: probe,
				JumpHost:       probe,
			},
			OpenAPI: &OpenAPIConfig{Spec: probe, BaseURL: probe},
		}},
		Resources: []Resource{{
			Name:    probe,
			Image:   probe,
			Network: probe,
			Env:     map[string]string{"RE1": probe},
		}},
	}

	// Resolve PROBE so expanded names stay clean; refs are still captured.
	expandStackVars(stack, VaultResolver(&mockVault{secrets: map[string]string{"PROBE": "x"}}))

	want := []string{
		"stack|name",
		"gateway|allowed_origins[0]",
		"gateway|auth.token",
		"network|name",
		"mcp-server|name",
		"mcp-server|image",
		"mcp-server|url",
		"mcp-server|network",
		"mcp-server|command[0]",
		"mcp-server|source.url",
		"mcp-server|source.path",
		"mcp-server|source.ref",
		"mcp-server|source.auth.ssh_key_path",
		"mcp-server|source.auth.ssh_user",
		"mcp-server|env.E1",
		"mcp-server|build_args.B1",
		"mcp-server|ssh.host",
		"mcp-server|ssh.user",
		"mcp-server|ssh.identityFile",
		"mcp-server|ssh.knownHostsFile",
		"mcp-server|ssh.jumpHost",
		"mcp-server|openapi.spec",
		"mcp-server|openapi.baseUrl",
		"resource|name",
		"resource|image",
		"resource|network",
		"resource|env.RE1",
	}

	got := map[string]bool{}
	for _, c := range stack.References["PROBE"] {
		got[string(c.Kind)+"|"+c.Field] = true
	}

	for _, w := range want {
		if !got[w] {
			t.Errorf("missing indexed reference site: %s (under-counting)", w)
		}
		delete(got, w)
	}
	for extra := range got {
		t.Errorf("unexpected indexed reference site: %s", extra)
	}

	// CredentialRef is deliberately left literal for clone-time resolution and
	// must not be indexed.
	if _, ok := stack.References["PROBE_CRED"]; ok {
		t.Error("source.auth.credential_ref must not be indexed")
	}
}

func TestExpandStackVars_OneHop(t *testing.T) {
	// X's value itself references Y. One-hop means we index X (it appears in the
	// stack) but never follow into X's value to index Y.
	stack := &Stack{
		MCPServers: []MCPServer{{
			Name: "api",
			Env:  map[string]string{"TOKEN": "${var:X}"},
		}},
	}
	expandStackVars(stack, VaultResolver(&mockVault{secrets: map[string]string{
		"X": "${var:Y}",
		"Y": "deep",
	}}))

	if _, ok := stack.References["X"]; !ok {
		t.Error("expected X to be indexed (direct stack reference)")
	}
	if _, ok := stack.References["Y"]; ok {
		t.Error("Y must NOT be indexed: it lives inside X's value, not the stack (one-hop)")
	}
}

func TestExpandStackVars_DedupesPerField(t *testing.T) {
	stack := &Stack{
		MCPServers: []MCPServer{{
			Name: "api",
			Env:  map[string]string{"E": "${var:DUP}-${var:DUP}"},
		}},
	}
	expandStackVars(stack, EnvResolver())

	if got := len(stack.References["DUP"]); got != 1 {
		t.Errorf("DUP consumers = %d, want 1 (same field counted once)", got)
	}
}

func TestExpandStackVars_BareEnvNotIndexed(t *testing.T) {
	stack := &Stack{
		MCPServers: []MCPServer{{
			Name: "api",
			Env: map[string]string{
				"A": "${PLAIN}",
				"B": "$BARE",
			},
		}},
	}
	expandStackVars(stack, EnvResolver())

	if len(stack.References) != 0 {
		t.Errorf("env-style references must not be indexed, got %v", stack.References)
	}
}

func TestExpandStackVars_TwoServersDistinctConsumers(t *testing.T) {
	stack := &Stack{
		MCPServers: []MCPServer{
			{Name: "github", Env: map[string]string{"TOKEN": "${var:SHARED}"}},
			{Name: "gitlab", Env: map[string]string{"TOKEN": "${var:SHARED}"}},
		},
	}
	expandStackVars(stack, EnvResolver())

	consumers := stack.References["SHARED"]
	if len(consumers) != 2 {
		t.Fatalf("SHARED consumers = %d, want 2", len(consumers))
	}
	names := []string{consumers[0].Name, consumers[1].Name}
	sort.Strings(names)
	if names[0] != "github" || names[1] != "gitlab" {
		t.Errorf("consumer names = %v, want [github gitlab]", names)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
