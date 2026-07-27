package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSecretsConfig_RoundTrip asserts the secrets block survives a load/save
// cycle without dropping fields, and that an unscoped entry written as a bare
// string comes back out as a bare string (Article IX back-compat).
func TestSecretsConfig_RoundTrip(t *testing.T) {
	src := `version: "1"
name: test
network:
  name: net
secrets:
  sets:
    - shared
    - name: github-creds
      servers:
        - github
    - name: db
      resources:
        - postgres
mcp-servers:
  - name: github
    image: alpine
    port: 3000
resources:
  - name: postgres
    image: postgres:16
`
	var stack Stack
	if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stack.Secrets == nil {
		t.Fatal("secrets block dropped on unmarshal")
	}
	if len(stack.Secrets.Sets) != 3 {
		t.Fatalf("got %d set refs, want 3", len(stack.Secrets.Sets))
	}
	if got := stack.Secrets.Sets[0]; got.Name != "shared" || got.Scoped() {
		t.Errorf("sets[0] = %+v, want unscoped 'shared'", got)
	}
	if got := stack.Secrets.Sets[1]; got.Name != "github-creds" ||
		len(got.Servers) != 1 || got.Servers[0] != "github" {
		t.Errorf("sets[1] = %+v", got)
	}
	if got := stack.Secrets.Sets[2]; got.Name != "db" ||
		len(got.Resources) != 1 || got.Resources[0] != "postgres" {
		t.Errorf("sets[2] = %+v", got)
	}

	out, err := yaml.Marshal(&stack)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// The shorthand entry must not be promoted to a mapping on the way out,
	// or every save would rewrite stacks that never opted into scoping.
	if !strings.Contains(string(out), "- shared\n") {
		t.Errorf("unscoped entry lost its scalar form on marshal:\n%s", out)
	}

	var reparsed Stack
	if err := yaml.Unmarshal(out, &reparsed); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if reparsed.Secrets == nil || len(reparsed.Secrets.Sets) != 3 {
		t.Fatalf("round-trip lost entries: %+v", reparsed.Secrets)
	}
	for i := range stack.Secrets.Sets {
		before, after := stack.Secrets.Sets[i], reparsed.Secrets.Sets[i]
		if before.Name != after.Name ||
			len(before.Servers) != len(after.Servers) ||
			len(before.Resources) != len(after.Resources) {
			t.Errorf("sets[%d] changed across round-trip: %+v -> %+v", i, before, after)
		}
	}
}

// TestSecretsConfig_BackCompat pins the pre-scoping shapes: no secrets block at
// all, and a block whose entries are all bare strings.
func TestSecretsConfig_BackCompat(t *testing.T) {
	t.Run("no secrets block is valid", func(t *testing.T) {
		src := `version: "1"
name: test
network:
  name: net
mcp-servers:
  - name: github
    image: alpine
    port: 3000
`
		var stack Stack
		if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if stack.Secrets != nil {
			t.Errorf("Secrets = %+v, want nil", stack.Secrets)
		}
		stack.SetDefaults()
		if err := Validate(&stack); err != nil {
			t.Errorf("validate: %v", err)
		}
	})

	t.Run("bare string entries stay unscoped", func(t *testing.T) {
		src := `version: "1"
name: test
network:
  name: net
secrets:
  sets:
    - dev
    - prod
mcp-servers:
  - name: github
    image: alpine
    port: 3000
`
		var stack Stack
		if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		stack.SetDefaults()
		if err := Validate(&stack); err != nil {
			t.Fatalf("validate: %v", err)
		}
		for _, ref := range stack.Secrets.Sets {
			if ref.Scoped() {
				t.Errorf("%q parsed as scoped; bare strings must fan out", ref.Name)
			}
			if !ref.InjectsIntoServer("anything") || !ref.InjectsIntoResource("anything") {
				t.Errorf("%q does not fan out", ref.Name)
			}
		}
	})
}

// TestSecretSetRef_ScopeFailsClosed pins the two ways a scoped entry could
// silently fan out instead of restricting. Both matter more than a normal
// parse error: this feature exists to withhold credentials, so a
// misunderstood scope must fail loudly rather than granting more access than
// the author asked for.
func TestSecretSetRef_ScopeFailsClosed(t *testing.T) {
	t.Run("a misspelled scope key is rejected, not ignored", func(t *testing.T) {
		// yaml.v3 drops unknown fields by default, which would leave this
		// entry unscoped and inject the set into every workload.
		src := `secrets:
  sets:
    - name: creds
      server:
        - github
`
		var stack Stack
		err := yaml.Unmarshal([]byte(src), &stack)
		if err == nil {
			t.Fatal("singular 'server:' was accepted; the entry would fan out to every workload")
		}
		if !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("error = %v, want it to name the unknown field", err)
		}
	})

	t.Run("an explicitly empty scope injects nowhere", func(t *testing.T) {
		src := `secrets:
  sets:
    - name: creds
      servers: []
`
		var stack Stack
		if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		ref := stack.Secrets.Sets[0]
		if !ref.Scoped() {
			t.Error("an explicit empty list read as unscoped")
		}
		if ref.InjectsIntoServer("anything") || ref.InjectsIntoResource("anything") {
			t.Error("an explicit empty scope still fans out")
		}
	})

	t.Run("a scope that reaches nothing is a validation error", func(t *testing.T) {
		src := `version: "1"
name: test
network:
  name: net
secrets:
  sets:
    - name: creds
      servers: []
mcp-servers:
  - name: github
    image: alpine
    port: 3000
`
		var stack Stack
		if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		stack.SetDefaults()
		err := Validate(&stack)
		if err == nil {
			t.Fatal("a set scoped to nothing passed validation")
		}
		if !strings.Contains(err.Error(), "injects nowhere") {
			t.Errorf("error = %v, want it to say the set injects nowhere", err)
		}
	})

	t.Run("a Go-built entry is scoped by its lists alone", func(t *testing.T) {
		// scopeDeclared is unexported and unset here, so Scoped() must still
		// see the populated list.
		ref := SecretSetRef{Name: "creds", Servers: []string{"github"}}
		if !ref.Scoped() || ref.InjectsIntoServer("other") {
			t.Errorf("programmatic entry not scoped: %+v", ref)
		}
	})
}

func TestSecretSetRef_UnmarshalYAML_RejectsOtherKinds(t *testing.T) {
	var ref SecretSetRef
	node := &yaml.Node{}
	if err := yaml.Unmarshal([]byte("- [nested]\n"), node); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	// node is a document -> sequence -> sequence; hand the inner sequence over.
	inner := node.Content[0].Content[0]
	if err := ref.UnmarshalYAML(inner); err == nil {
		t.Error("expected a nested sequence to be rejected")
	}
}

func TestValidateSecrets(t *testing.T) {
	base := func(sets []SecretSetRef) *Stack {
		s := &Stack{
			Version:    "1",
			Name:       "test",
			Network:    Network{Name: "net"},
			Secrets:    &Secrets{Sets: sets},
			MCPServers: []MCPServer{{Name: "github", Image: "alpine", Port: 3000}},
			Resources:  []Resource{{Name: "postgres", Image: "postgres:16"}},
		}
		s.SetDefaults()
		return s
	}

	tests := []struct {
		name    string
		sets    []SecretSetRef
		wantErr string
	}{
		{
			name: "valid scoped entry",
			sets: []SecretSetRef{{Name: "dev", Servers: []string{"github"}}},
		},
		{
			name: "valid resource scope",
			sets: []SecretSetRef{{Name: "db", Resources: []string{"postgres"}}},
		},
		{
			name:    "unknown server",
			sets:    []SecretSetRef{{Name: "dev", Servers: []string{"nope"}}},
			wantErr: "unknown MCP server 'nope'",
		},
		{
			name:    "unknown resource",
			sets:    []SecretSetRef{{Name: "dev", Resources: []string{"nope"}}},
			wantErr: "unknown resource 'nope'",
		},
		{
			name:    "empty name on a scoped entry",
			sets:    []SecretSetRef{{Servers: []string{"github"}}},
			wantErr: "set name is required",
		},
		{
			// Accepted before scoping existed, so rejecting it now would break
			// stacks that never opted in (Article IX). Repeating a bare name is
			// idempotent: same members, same fan-out.
			name: "duplicate bare names stay valid",
			sets: []SecretSetRef{{Name: "dev"}, {Name: "dev"}},
		},
		{
			// Two scopes for one set inject the union, which reads as if each
			// entry confined the set while together they widen it.
			name: "duplicate scoped names are rejected",
			sets: []SecretSetRef{
				{Name: "dev", Servers: []string{"github"}},
				{Name: "dev", Resources: []string{"postgres"}},
			},
			wantErr: "already listed",
		},
		{
			// The worst shape: the bare entry silently fans out a set the
			// scoped entry declares as confined.
			name: "a bare entry beside a scoped one is rejected",
			sets: []SecretSetRef{
				{Name: "dev"},
				{Name: "dev", Servers: []string{"github"}},
			},
			wantErr: "already listed",
		},
		{
			name: "distinct scoped sets are fine",
			sets: []SecretSetRef{
				{Name: "dev", Servers: []string{"github"}},
				{Name: "db", Resources: []string{"postgres"}},
			},
		},
		{
			name: "empty bare name stays valid",
			sets: []SecretSetRef{{Name: ""}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(base(tc.sets))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestSecrets_ExtendsInheritance covers the top-level inherit rule at
// mergeStacks: a child with no secrets block adopts the parent's, and a child
// that declares one keeps its own entirely.
func TestSecrets_ExtendsInheritance(t *testing.T) {
	parent := Stack{Secrets: &Secrets{Sets: []SecretSetRef{
		{Name: "shared"},
		{Name: "db", Resources: []string{"postgres"}},
	}}}

	t.Run("child without a block inherits the parent's", func(t *testing.T) {
		child := Stack{}
		mergeStacks(&child, &parent)
		if child.Secrets == nil {
			t.Fatal("secrets not inherited")
		}
		if len(child.Secrets.Sets) != 2 {
			t.Fatalf("got %d sets, want 2", len(child.Secrets.Sets))
		}
		if got := child.Secrets.Sets[1]; !got.Scoped() || got.Resources[0] != "postgres" {
			t.Errorf("scoping lost through inheritance: %+v", got)
		}
	})

	t.Run("child block overrides the parent's", func(t *testing.T) {
		child := Stack{Secrets: &Secrets{Sets: []SecretSetRef{{Name: "own"}}}}
		mergeStacks(&child, &parent)
		if len(child.Secrets.Sets) != 1 || child.Secrets.Sets[0].Name != "own" {
			t.Errorf("child secrets = %+v, want its own block untouched", child.Secrets.Sets)
		}
	})
}
