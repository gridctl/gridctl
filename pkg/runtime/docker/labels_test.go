package docker

import "testing"

func TestManagedLabels(t *testing.T) {
	tests := []struct {
		name        string
		stack       string
		serverName  string
		isMCPServer bool
		wantLabels  map[string]string
	}{
		{
			name:        "MCP server labels",
			stack:       "test-topo",
			serverName:  "my-server",
			isMCPServer: true,
			wantLabels: map[string]string{
				LabelManaged:   "true",
				LabelStack:  "test-topo",
				LabelMCPServer: "my-server",
			},
		},
		{
			name:        "resource labels",
			stack:       "prod",
			serverName:  "postgres",
			isMCPServer: false,
			wantLabels: map[string]string{
				LabelManaged:  "true",
				LabelStack: "prod",
				LabelResource: "postgres",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			labels := ManagedLabels(tc.stack, tc.serverName, tc.isMCPServer)

			if len(labels) != len(tc.wantLabels) {
				t.Errorf("got %d labels, want %d", len(labels), len(tc.wantLabels))
			}

			for k, want := range tc.wantLabels {
				if got := labels[k]; got != want {
					t.Errorf("label %q = %q, want %q", k, got, want)
				}
			}

			// Verify MCP server vs resource label exclusivity
			if tc.isMCPServer {
				if _, ok := labels[LabelResource]; ok {
					t.Error("MCP server should not have resource label")
				}
			} else {
				if _, ok := labels[LabelMCPServer]; ok {
					t.Error("resource should not have MCP server label")
				}
			}
		})
	}
}


func TestContainerName(t *testing.T) {
	tests := []struct {
		stack string
		name  string
		want  string
	}{
		{"my-topo", "server1", "gridctl-my-topo-server1"},
		{"prod", "postgres", "gridctl-prod-postgres"},
		{"dev", "mcp-test", "gridctl-dev-mcp-test"},
	}

	for _, tc := range tests {
		t.Run(tc.stack+"_"+tc.name, func(t *testing.T) {
			got := ContainerName(tc.stack, tc.name)
			if got != tc.want {
				t.Errorf("ContainerName(%q, %q) = %q, want %q", tc.stack, tc.name, got, tc.want)
			}
		})
	}
}
