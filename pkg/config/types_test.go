package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStack_NeedsContainerRuntime(t *testing.T) {
	tests := []struct {
		name string
		stack Stack
		want bool
	}{
		{
			name:  "empty stack",
			stack: Stack{Name: "empty"},
			want:  false,
		},
		{
			name: "external servers only",
			stack: Stack{
				Name: "ext",
				MCPServers: []MCPServer{
					{Name: "api", URL: "http://localhost:8080"},
				},
			},
			want: false,
		},
		{
			name: "local process servers only",
			stack: Stack{
				Name: "local",
				MCPServers: []MCPServer{
					{Name: "stdio", Command: []string{"npx", "server"}},
				},
			},
			want: false,
		},
		{
			name: "SSH servers only",
			stack: Stack{
				Name: "ssh",
				MCPServers: []MCPServer{
					{Name: "remote", SSH: &SSHConfig{Host: "host", User: "user"}, Command: []string{"server"}},
				},
			},
			want: false,
		},
		{
			name: "OpenAPI servers only",
			stack: Stack{
				Name: "openapi",
				MCPServers: []MCPServer{
					{Name: "api", OpenAPI: &OpenAPIConfig{Spec: "spec.yaml"}},
				},
			},
			want: false,
		},
		{
			name: "mixed non-container servers",
			stack: Stack{
				Name: "mixed-nocontainer",
				MCPServers: []MCPServer{
					{Name: "ext", URL: "http://localhost:8080"},
					{Name: "local", Command: []string{"npx", "server"}},
					{Name: "ssh", SSH: &SSHConfig{Host: "host", User: "user"}, Command: []string{"cmd"}},
					{Name: "api", OpenAPI: &OpenAPIConfig{Spec: "spec.yaml"}},
				},
			},
			want: false,
		},
		{
			name: "image-based server",
			stack: Stack{
				Name: "container",
				MCPServers: []MCPServer{
					{Name: "weather", Image: "mcp/weather:latest", Port: 3000},
				},
			},
			want: true,
		},
		{
			name: "source-based server",
			stack: Stack{
				Name: "source",
				MCPServers: []MCPServer{
					{Name: "custom", Source: &Source{Type: "git", URL: "https://github.com/example/repo"}},
				},
			},
			want: true,
		},
		{
			name: "has resources",
			stack: Stack{
				Name:      "with-resources",
				Resources: []Resource{{Name: "postgres", Image: "postgres:16"}},
			},
			want: true,
		},
		{
			name: "mixed container and non-container",
			stack: Stack{
				Name: "mixed",
				MCPServers: []MCPServer{
					{Name: "local", Command: []string{"npx", "server"}},
					{Name: "weather", Image: "mcp/weather:latest", Port: 3000},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stack.NeedsContainerRuntime()
			if got != tt.want {
				t.Errorf("NeedsContainerRuntime() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStack_ContainerWorkloads(t *testing.T) {
	stack := Stack{
		Name: "test",
		MCPServers: []MCPServer{
			{Name: "weather", Image: "mcp/weather:latest", Port: 3000},
			{Name: "local", Command: []string{"npx", "server"}},
		},
		Resources: []Resource{
			{Name: "postgres", Image: "postgres:16"},
		},
	}

	workloads := stack.ContainerWorkloads()
	if len(workloads) != 2 {
		t.Fatalf("expected 2 container workloads, got %d", len(workloads))
	}
}

func TestMCPServer_ReplicasYAMLRoundTrip(t *testing.T) {
	tests := []struct {
		name         string
		yamlIn       string
		wantReplicas int
		wantPolicy   string
	}{
		{
			name: "no replica fields",
			yamlIn: `name: s
image: alpine
port: 3000
`,
			wantReplicas: 0,
			wantPolicy:   "",
		},
		{
			name: "explicit replicas and policy",
			yamlIn: `name: s
image: alpine
port: 3000
replicas: 3
replica_policy: least-connections
`,
			wantReplicas: 3,
			wantPolicy:   "least-connections",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s MCPServer
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &s); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if s.Replicas != tc.wantReplicas {
				t.Errorf("Replicas = %d, want %d", s.Replicas, tc.wantReplicas)
			}
			if s.ReplicaPolicy != tc.wantPolicy {
				t.Errorf("ReplicaPolicy = %q, want %q", s.ReplicaPolicy, tc.wantPolicy)
			}

			out, err := yaml.Marshal(&s)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got MCPServer
			if err := yaml.Unmarshal(out, &got); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			if got.Replicas != s.Replicas || got.ReplicaPolicy != s.ReplicaPolicy {
				t.Errorf("round-trip mismatch: got %+v, want %+v", got, s)
			}
		})
	}
}

func TestTracingConfig_MaxTracesYAMLRoundTrip(t *testing.T) {
	tests := []struct {
		name          string
		yamlIn        string
		wantMaxTraces int
	}{
		{
			name: "no max_traces field",
			yamlIn: `enabled: true
`,
			wantMaxTraces: 0,
		},
		{
			name: "explicit max_traces",
			yamlIn: `enabled: true
max_traces: 250
`,
			wantMaxTraces: 250,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var tcfg TracingConfig
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &tcfg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if tcfg.MaxTraces != tc.wantMaxTraces {
				t.Errorf("MaxTraces = %d, want %d", tcfg.MaxTraces, tc.wantMaxTraces)
			}

			out, err := yaml.Marshal(&tcfg)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got TracingConfig
			if err := yaml.Unmarshal(out, &got); err != nil {
				t.Fatalf("re-unmarshal: %v", err)
			}
			if got.MaxTraces != tcfg.MaxTraces {
				t.Errorf("round-trip mismatch: got %d, want %d", got.MaxTraces, tcfg.MaxTraces)
			}
		})
	}
}

func TestTracingConfig_EnabledYAMLTriState(t *testing.T) {
	tests := []struct {
		name   string
		yamlIn string
		want   *bool
	}{
		{
			name: "omitted enabled stays nil (inherits default)",
			yamlIn: `sampling: 0.5
`,
			want: nil,
		},
		{
			name: "explicit false",
			yamlIn: `enabled: false
`,
			want: boolPtr(false),
		},
		{
			name: "explicit true",
			yamlIn: `enabled: true
`,
			want: boolPtr(true),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var tcfg TracingConfig
			if err := yaml.Unmarshal([]byte(tc.yamlIn), &tcfg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !boolPtrEqual(tcfg.Enabled, tc.want) {
				t.Errorf("Enabled = %v, want %v", tcfg.Enabled, tc.want)
			}
		})
	}
}

func TestStack_NonContainerWorkloads(t *testing.T) {
	stack := Stack{
		Name: "test",
		MCPServers: []MCPServer{
			{Name: "weather", Image: "mcp/weather:latest", Port: 3000},
			{Name: "local", Command: []string{"npx", "server"}},
			{Name: "ext", URL: "http://localhost:8080"},
		},
	}

	workloads := stack.NonContainerWorkloads()
	if len(workloads) != 2 {
		t.Fatalf("expected 2 non-container workloads, got %d", len(workloads))
	}
}

func TestStack_ModelAttribution(t *testing.T) {
	tests := []struct {
		name  string
		stack *Stack
		want  map[string]string
	}{
		{
			name:  "nil stack",
			stack: nil,
			want:  nil,
		},
		{
			name:  "no models configured",
			stack: &Stack{MCPServers: []MCPServer{{Name: "a"}, {Name: "b"}}},
			want:  nil,
		},
		{
			name: "per-server model only",
			stack: &Stack{MCPServers: []MCPServer{
				{Name: "a", Model: "claude-opus-4-7"},
				{Name: "b"},
			}},
			want: map[string]string{"a": "claude-opus-4-7"},
		},
		{
			name: "default model fills unset servers",
			stack: &Stack{
				Gateway: &GatewayConfig{DefaultModel: "claude-haiku-4-5"},
				MCPServers: []MCPServer{
					{Name: "a", Model: "claude-opus-4-7"},
					{Name: "b"},
				},
			},
			want: map[string]string{
				"a": "claude-opus-4-7",
				"b": "claude-haiku-4-5",
			},
		},
		{
			name: "default model only",
			stack: &Stack{
				Gateway:    &GatewayConfig{DefaultModel: "claude-haiku-4-5"},
				MCPServers: []MCPServer{{Name: "a"}, {Name: "b"}},
			},
			want: map[string]string{
				"a": "claude-haiku-4-5",
				"b": "claude-haiku-4-5",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stack.ModelAttribution()
			if len(got) != len(tt.want) {
				t.Fatalf("ModelAttribution() = %v, want %v", got, tt.want)
			}
			for server, model := range tt.want {
				if got[server] != model {
					t.Errorf("ModelAttribution()[%q] = %q, want %q", server, got[server], model)
				}
			}
		})
	}
}

func TestMCPServer_ModelYAMLRoundTrip(t *testing.T) {
	in := MCPServer{Name: "priced", Image: "mcp/priced:latest", Model: "claude-opus-4-7"}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out MCPServer
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Model != "claude-opus-4-7" {
		t.Errorf("Model round-trip = %q, want %q", out.Model, "claude-opus-4-7")
	}

	// A server without a model must not serialize the field (Article IX:
	// the zero value is indistinguishable from a pre-model stack.yaml).
	raw, err = yaml.Marshal(MCPServer{Name: "plain"})
	if err != nil {
		t.Fatalf("marshal plain: %v", err)
	}
	if strings.Contains(string(raw), "model:") {
		t.Errorf("zero-value Model serialized: %s", raw)
	}
}

func TestStack_ClientModelAttribution(t *testing.T) {
	tests := []struct {
		name  string
		stack *Stack
		want  map[string]string
	}{
		{
			name:  "nil stack",
			stack: nil,
			want:  nil,
		},
		{
			name:  "no client models",
			stack: &Stack{MCPServers: []MCPServer{{Name: "a"}}},
			want:  nil,
		},
		{
			name: "declared clients",
			stack: &Stack{ClientModels: map[string]string{
				"claude-code": "claude-opus-4-7",
				"gemini-cli":  "gemini-2.5-pro",
			}},
			want: map[string]string{
				"claude-code": "claude-opus-4-7",
				"gemini-cli":  "gemini-2.5-pro",
			},
		},
		{
			name: "empty values skipped",
			stack: &Stack{ClientModels: map[string]string{
				"claude-code": "claude-opus-4-7",
				"gemini-cli":  "",
			}},
			want: map[string]string{"claude-code": "claude-opus-4-7"},
		},
		{
			name:  "all values empty returns nil",
			stack: &Stack{ClientModels: map[string]string{"gemini-cli": ""}},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.stack.ClientModelAttribution()
			if len(got) != len(tt.want) {
				t.Fatalf("ClientModelAttribution() = %v, want %v", got, tt.want)
			}
			for client, model := range tt.want {
				if got[client] != model {
					t.Errorf("ClientModelAttribution()[%q] = %q, want %q", client, got[client], model)
				}
			}
		})
	}
}

func TestStack_ClientModelsYAMLRoundTrip(t *testing.T) {
	in := Stack{
		Name:         "test",
		ClientModels: map[string]string{"claude-code": "claude-opus-4-7"},
	}
	raw, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Stack
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ClientModels["claude-code"] != "claude-opus-4-7" {
		t.Errorf("ClientModels round-trip = %v", out.ClientModels)
	}

	// A stack without client models must not serialize the key (Article IX:
	// the zero value is indistinguishable from a pre-feature stack.yaml).
	raw, err = yaml.Marshal(Stack{Name: "plain"})
	if err != nil {
		t.Fatalf("marshal plain: %v", err)
	}
	if strings.Contains(string(raw), "client_models") {
		t.Errorf("zero-value ClientModels serialized: %s", raw)
	}
}

// TestStack_ClientModelsAccessInert pins the design constraint that drove
// the top-level client_models map: declaring pricing models must never
// create or imply an access policy. The clients: block stays nil.
func TestStack_ClientModelsAccessInert(t *testing.T) {
	src := `
name: pricing-only
client_models:
  claude-code: claude-opus-4-7
mcp-servers:
  - name: github
    image: mcp/github:latest
    port: 3000
`
	var stack Stack
	if err := yaml.Unmarshal([]byte(src), &stack); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if stack.Clients != nil {
		t.Error("client_models must not materialize a clients: access block")
	}
	if stack.ClientModelAttribution()["claude-code"] != "claude-opus-4-7" {
		t.Errorf("attribution = %v", stack.ClientModelAttribution())
	}
}
