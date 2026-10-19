package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretRef_StaysLiteralAndIndexed(t *testing.T) {
	stack := &Stack{
		Name: "demo",
		MCPServers: []MCPServer{{
			Name: "netbox",
			Env:  map[string]string{"NETBOX_TOKEN": "Bearer ${secret:op/a/b}"},
		}},
		Secrets: &Secrets{Providers: []SecretProvider{{Name: "op", Type: "onepassword"}}},
	}
	expandStackVarsResolved(stack, func(name string, _ bool) ResolutionResult {
		return ResolutionResult{Verdict: ResolutionUnset}
	})
	if got := stack.MCPServers[0].Env["NETBOX_TOKEN"]; got != "Bearer ${secret:op/a/b}" {
		t.Fatalf("reference was resolved at load: %q", got)
	}
	if !ContainsExpansion("${secret:op/a/b}") {
		t.Fatal("ContainsExpansion should match a secret reference")
	}
	if len(stack.SecretRefs) != 1 || stack.SecretRefs[0].Key != "secret:op/a/b" {
		t.Fatalf("SecretRefs = %#v", stack.SecretRefs)
	}
	if len(stack.References["secret:op/a/b"]) != 1 {
		t.Fatalf("index = %#v", stack.References)
	}
	usage, _ := BuildVariableUsage(stack, nil)
	if _, ok := usage["secret:op/a/b"]; ok {
		t.Fatal("BuildVariableUsage included a secret reference")
	}
}

func TestSecretRef_ParityAcrossExpandableFields(t *testing.T) {
	const probe = "${secret:op/a/b}"
	stack := &Stack{
		Name: probe,
		Gateway: &GatewayConfig{
			AllowedOrigins: []string{probe},
			Bind:           probe,
			Auth:           &AuthConfig{Token: probe},
		},
		Network:  Network{Name: probe},
		Networks: []Network{{Name: probe}},
		MCPServers: []MCPServer{{
			Name:    "srv",
			Image:   probe,
			Env:     map[string]string{"TOKEN": probe},
			Command: []string{probe},
			Auth:    &ServerAuth{Token: probe, ClientSecret: probe},
			A2A:     &A2AConfig{Auth: &A2AAuth{Token: probe}},
		}},
		Resources: []Resource{{Name: "db", Env: map[string]string{"PASSWORD": probe}}},
		Secrets:   &Secrets{Providers: []SecretProvider{{Name: "op", Type: "onepassword"}}},
	}
	expandStackVarsResolved(stack, func(string, bool) ResolutionResult {
		return ResolutionResult{Verdict: ResolutionUnset}
	})
	want := []string{"name", "gateway.auth.token", "mcp-servers[0].env.TOKEN", "resources[0].env.PASSWORD"}
	got := map[string]bool{}
	for _, c := range stack.References["secret:op/a/b"] {
		got[secretConsumerField(stack, c)] = true
	}
	for _, field := range []string{"name", "gateway.auth.token", "gateway.bind", "gateway.allowed_origins[0]", "network.name", "mcp-servers[0].env.TOKEN", "mcp-servers[0].command[0]", "mcp-servers[0].auth.token", "mcp-servers[0].a2a.auth.token", "resources[0].env.PASSWORD"} {
		if !got[field] {
			t.Errorf("missing indexed site %s", field)
		}
	}
	_ = want
	if stack.Name != probe {
		t.Fatalf("name was expanded: %q", stack.Name)
	}
}

func TestValidateSecrets_ProvidersAndOperands(t *testing.T) {
	base := func(extra string) string {
		return "version: \"1\"\nname: demo\n" + extra + "mcp-servers:\n  - name: netbox\n    command: [\"true\"]\n    env:\n      TOKEN: ${secret:op/a/b}\n"
	}
	cases := []struct {
		name    string
		body    string
		field   string
		message string
	}{
		{"undeclared", base(""), "mcp-servers[0].env.TOKEN", `references undeclared secret provider "op"`},
		{"missing placeholder", base("secrets:\n  providers:\n    - name: op\n      type: exec\n      command: [\"echo\", \"nope\"]\n"), "secrets.providers[0].command", "command must contain {{path}}"},
		{"timeout", base("secrets:\n  providers:\n    - name: op\n      type: exec\n      command: [\"echo\", \"{{path}}\"]\n      timeout: 1ms\n"), "secrets.providers[0].timeout", "between 1s and 5m"},
		{"relative token", base("secrets:\n  providers:\n    - name: op\n      type: onepassword\n      token_file: relative/op\n"), "secrets.providers[0].token_file", "token_file must be absolute or start with ~/"},
		{"operand", "version: \"1\"\nname: demo\nsecrets:\n  providers:\n    - name: op\n      type: onepassword\nmcp-servers:\n  - name: netbox\n    command: [\"true\"]\n    env:\n      TOKEN: ${secret:op/a/b:-fallback}\n", "mcp-servers[0].env.TOKEN", "do not take default or replacement operands"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "stack.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, loadErr := LoadStack(path)
			_, result, validateErr := ValidateStackFile(path)
			if validateErr != nil {
				t.Fatal(validateErr)
			}
			if loadErr == nil || result == nil || result.ErrorCount == 0 {
				t.Fatalf("expected both loaders to fail, load=%v validate=%#v", loadErr, result)
			}
			var loadErrors ValidationErrors
			if !asValidation(loadErr, &loadErrors) {
				t.Fatalf("load error is not validation: %v", loadErr)
			}
			assertField(t, loadErrors, tc.field, tc.message)
			var validateErrors ValidationErrors
			for _, issue := range result.Issues {
				if issue.Severity == SeverityError {
					validateErrors = append(validateErrors, ValidationError{Field: issue.Field, Message: issue.Message})
				}
			}
			assertField(t, validateErrors, tc.field, tc.message)
		})
	}
}

func TestValidateSecrets_ExtendsInheritsProvider(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "parent.yaml")
	child := filepath.Join(dir, "child.yaml")
	parentBody := "version: \"1\"\nname: parent\nsecrets:\n  providers:\n    - name: op\n      type: onepassword\nmcp-servers: []\n"
	childBody := "version: \"1\"\nname: child\nextends: parent.yaml\nsecrets:\n  sets:\n    - shared\nmcp-servers:\n  - name: netbox\n    command: [\"true\"]\n    env:\n      TOKEN: ${secret:op/a/b}\n"
	if err := os.WriteFile(parent, []byte(parentBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte(childBody), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadStack(child)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Secrets.Providers) != 1 || loaded.Secrets.Providers[0].Name != "op" {
		t.Fatalf("providers = %#v", loaded.Secrets.Providers)
	}
	if _, result, err := ValidateStackFile(child); err != nil || result.ErrorCount != 0 {
		t.Fatalf("validate extends child: err=%v errors=%d", err, result.ErrorCount)
	}
}

func TestMergeStacks_ProviderNameChildWins(t *testing.T) {
	child := &Stack{Secrets: &Secrets{
		Sets:      []SecretSetRef{{Name: "mine"}},
		Providers: []SecretProvider{{Name: "op", Type: "exec", Command: []string{"child", "{{path}}"}}},
	}}
	parent := &Stack{Secrets: &Secrets{
		Sets:      []SecretSetRef{{Name: "parent-set"}},
		Providers: []SecretProvider{{Name: "op", Type: "onepassword"}, {Name: "bao", Type: "exec", Command: []string{"{{path}}"}}},
	}}
	mergeStacks(child, parent)
	if len(child.Secrets.Sets) != 1 || child.Secrets.Sets[0].Name != "mine" {
		t.Fatalf("sets were inherited: %#v", child.Secrets.Sets)
	}
	if len(child.Secrets.Providers) != 2 {
		t.Fatalf("providers = %#v", child.Secrets.Providers)
	}
	if child.Secrets.Providers[0].Type != "exec" || child.Secrets.Providers[1].Name != "bao" {
		t.Fatalf("providers = %#v", child.Secrets.Providers)
	}
}

func TestExport_SecretAndStoreReferences(t *testing.T) {
	dir := t.TempDir()
	body := `version: "1"
name: demo
secrets:
  providers:
    - name: op
      type: onepassword
      token_file: /tmp/op-token
gateway:
  auth:
    type: bearer
    token: ${secret:op/gw/token}
mcp-servers:
  - name: netbox
    command: ["true"]
    env:
      NETBOX_TOKEN: ${var:NETBOX}
      VAULT_TOKEN: ${vault:OLD}
      SECRET_TOKEN: ${secret:op/a/b}
    auth:
      type: bearer
      token: ${var:AUTH}
      client_secret: ${vault:CLIENT}
    a2a:
      card: https://example.invalid
      endpoint: https://example.invalid
      auth:
        token: ${secret:op/a2a/token}
`
	path := filepath.Join(dir, "stack.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	exported, _, err := ExportStack(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if exported.MCPServers[0].Env["SECRET_TOKEN"] != "${secret:op/a/b}" {
		t.Fatalf("secret ref rewritten: %#v", exported.MCPServers[0].Env)
	}
	if exported.Secrets == nil || exported.Secrets.Providers[0].TokenFile != "/tmp/op-token" {
		t.Fatalf("providers = %#v", exported.Secrets)
	}
	if exported.Gateway.Auth.Token != "${secret:op/gw/token}" {
		t.Fatalf("gateway token = %q", exported.Gateway.Auth.Token)
	}

	refused := strings.Replace(body, "NETBOX_TOKEN: ${var:NETBOX}", "NETBOX_TOKEN: ${var:NETBOX:-x}", 1)
	refusedPath := filepath.Join(dir, "refused.yaml")
	if err := os.WriteFile(refusedPath, []byte(refused), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ExportStack(t.Context(), refusedPath); err == nil || !strings.Contains(err.Error(), "operand") {
		t.Fatalf("operand export err = %v", err)
	}
}

func TestPlan_SecretChangedOmitsValues(t *testing.T) {
	left := MCPServer{Name: "netbox", Env: map[string]string{"TOKEN": "${secret:op/a/b}", "PLAIN": "same"}}
	right := MCPServer{Name: "netbox", Env: map[string]string{"TOKEN": "${secret:op/a/c}", "PLAIN": "same"}}
	details := compareMCPServers(left, right)
	joined := strings.Join(details, ";")
	if !strings.Contains(joined, "secret changed") || strings.Contains(joined, "env changed") {
		t.Fatalf("details = %#v", details)
	}
	if strings.Contains(joined, "op/a") {
		t.Fatalf("value leaked: %s", joined)
	}
	both := compareMCPServers(left, MCPServer{Name: "netbox", Env: map[string]string{"TOKEN": "${secret:op/a/c}", "PLAIN": "other"}})
	joined = strings.Join(both, ";")
	if !strings.Contains(joined, "secret changed") || !strings.Contains(joined, "env changed") {
		t.Fatalf("mixed details = %#v", both)
	}
}

func asValidation(err error, out *ValidationErrors) bool {
	ve, ok := err.(ValidationErrors)
	if !ok {
		return false
	}
	*out = ve
	return true
}

func assertField(t *testing.T, errs ValidationErrors, field, message string) {
	t.Helper()
	for _, err := range errs {
		if err.Field == field && strings.Contains(err.Message, message) {
			return
		}
	}
	t.Fatalf("missing %s containing %q in %#v", field, message, errs)
}
