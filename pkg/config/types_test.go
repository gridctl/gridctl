package config

import (
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
