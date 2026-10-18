package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func policyStack(policy *PolicyConfig) *Stack {
	return &Stack{
		Name:    "test",
		Network: Network{Name: "net"},
		MCPServers: []MCPServer{
			{Name: "vault", Image: "alpine", Port: 3000},
			{Name: "pager", Image: "alpine", Port: 3001},
		},
		Policy: policy,
	}
}

func TestValidate_Policy(t *testing.T) {
	tests := []struct {
		name    string
		policy  *PolicyConfig
		wantErr bool
		errMsg  string
	}{
		{name: "omitted block is valid", policy: nil},
		{name: "empty block is valid", policy: &PolicyConfig{}},
		{
			name: "valid rules and glob",
			policy: &PolicyConfig{
				Default:          "allow",
				BlockDestructive: true,
				Rules: []PolicyRule{
					{Match: "vault__*", Action: "block"},
					{Match: "vault__list_paths", Action: "allow"},
					{Match: "pager__*", Action: "require_approval"},
				},
			},
		},
		{
			name:    "invalid default",
			policy:  &PolicyConfig{Default: "deny"},
			wantErr: true,
			errMsg:  "policy.default",
		},
		{
			name:    "empty match",
			policy:  &PolicyConfig{Rules: []PolicyRule{{Match: "", Action: "block"}}},
			wantErr: true,
			errMsg:  "policy.rules[0].match",
		},
		{
			name:    "unparseable glob",
			policy:  &PolicyConfig{Rules: []PolicyRule{{Match: "vault__[", Action: "block"}}},
			wantErr: true,
			errMsg:  "invalid glob pattern",
		},
		{
			name:    "unknown action",
			policy:  &PolicyConfig{Rules: []PolicyRule{{Match: "vault__*", Action: "ask"}}},
			wantErr: true,
			errMsg:  "policy.rules[0].action",
		},
		{
			name:    "literal names undeclared server",
			policy:  &PolicyConfig{Rules: []PolicyRule{{Match: "slack__post", Action: "block"}}},
			wantErr: true,
			errMsg:  "unknown MCP server 'slack'",
		},
		{
			name:    "literal is not prefixed",
			policy:  &PolicyConfig{Rules: []PolicyRule{{Match: "read_secret", Action: "block"}}},
			wantErr: true,
			errMsg:  "must be a prefixed name",
		},
		{
			name:   "glob with undeclared prefix passes",
			policy: &PolicyConfig{Rules: []PolicyRule{{Match: "slack__*", Action: "block"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(policyStack(tt.policy))
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("err = %v, want substring %q", err, tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateWithIssues_RequireApprovalWarns(t *testing.T) {
	result := ValidateWithIssues(policyStack(&PolicyConfig{
		Rules: []PolicyRule{
			{Match: "vault__*", Action: "block"},
			{Match: "pager__manage", Action: "require_approval"},
		},
	}))
	if !result.Valid {
		t.Fatalf("require_approval must not be an error: %+v", result.Issues)
	}
	if result.WarningCount != 1 {
		t.Fatalf("warning count = %d, want 1", result.WarningCount)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Field == "policy.rules[1].action" && strings.Contains(issue.Message, "enforced as block") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing require_approval warning: %+v", result.Issues)
	}
}

func TestPolicy_YAMLRoundTrip(t *testing.T) {
	src := "name: demo\npolicy: {}\nmcp-servers: []\n"
	var stack Stack
	if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
		t.Fatal(err)
	}
	if stack.Policy == nil {
		t.Fatal("policy: {} must unmarshal to a non-nil pointer")
	}
	if stack.Policy.Default != "" || len(stack.Policy.Rules) != 0 || stack.Policy.BlockDestructive {
		t.Fatalf("empty policy = %+v", stack.Policy)
	}

	omitted := "name: demo\nmcp-servers: []\n"
	var bare Stack
	if err := yaml.Unmarshal([]byte(omitted), &bare); err != nil {
		t.Fatal(err)
	}
	if bare.Policy != nil {
		t.Fatal("omitted policy must stay nil")
	}
}

func TestPolicyMatchLiteral(t *testing.T) {
	if !PolicyMatchLiteral("vault__read_secret") {
		t.Fatal("prefixed name is literal")
	}
	if PolicyMatchLiteral("vault__*") || PolicyMatchLiteral("vault__?") || PolicyMatchLiteral("vault__[ab]") {
		t.Fatal("metacharacters are not literal")
	}
}
