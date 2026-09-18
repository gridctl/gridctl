package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func a2aStack() *Stack {
	s := &Stack{Name: "test", Experimental: map[string]bool{"a2a": true}, MCPServers: []MCPServer{{Name: "agent", A2A: &A2AConfig{Card: "https://agent.example/card"}}}}
	s.SetDefaults()
	return s
}

func TestValidate_A2A(t *testing.T) {
	t.Setenv("GRIDCTL_EXPERIMENTAL_A2A", "true")
	for _, tc := range []struct {
		name, field string
		edit        func(*MCPServer)
	}{
		{"valid", "", func(*MCPServer) {}},
		{"missing card", ".a2a.card", func(s *MCPServer) { s.A2A.Card = "" }},
		{"insecure card", ".a2a.card", func(s *MCPServer) { s.A2A.Card = "http://agent.example/card" }},
		{"loopback", "", func(s *MCPServer) { s.A2A.Card = "http://[::1]/card" }},
		{"endpoint", ".a2a.endpoint", func(s *MCPServer) { s.A2A.Endpoint = "file:///rpc" }},
		{"dialect", ".a2a.dialect", func(s *MCPServer) { s.A2A.Dialect = "0.2" }},
		{"profile", ".a2a.profile", func(s *MCPServer) { s.A2A.Profile = "other" }},
		{"timeout", ".a2a.timeout", func(s *MCPServer) { s.A2A.Timeout = "0s" }},
		{"auth", ".a2a.auth.type", func(s *MCPServer) { s.A2A.Auth = &A2AAuth{Type: "oauth"} }},
		{"token", ".a2a.auth.token", func(s *MCPServer) { s.A2A.Auth = &A2AAuth{Type: "bearer"} }},
		{"bearer", "", func(s *MCPServer) { s.A2A.Auth = &A2AAuth{Type: "bearer", Token: "${var:A2A_TOKEN}"} }},
		{"transport", ".transport", func(s *MCPServer) { s.Transport = "http" }},
		{"port", ".port", func(s *MCPServer) { s.Port = 8000 }},
		{"network", ".network", func(s *MCPServer) { s.Network = "net" }},
		{"replicas", ".replicas", func(s *MCPServer) { s.Replicas = 2 }},
		{"autoscale", ".autoscale", func(s *MCPServer) { s.Replicas = 0; s.Autoscale = &AutoscaleConfig{Min: 0, Max: 2, TargetInFlight: 1} }},
		{"execution", ".execution", func(s *MCPServer) { s.Execution = &ExecutionConfig{Mode: "hardened"} }},
		{"legacy auth", ".auth", func(s *MCPServer) { s.Auth = &ServerAuth{Type: "bearer", Token: "${var:A2A_TOKEN}"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := a2aStack()
			tc.edit(&s.MCPServers[0])
			err := Validate(s)
			if tc.field == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.field)
			}
		})
	}
}

func TestValidate_A2AFlag(t *testing.T) {
	for _, tc := range []struct {
		env        string
		yaml, want bool
	}{
		{"", false, false}, {"", true, true}, {"true", false, true}, {"false", true, false},
	} {
		t.Run(tc.env+"-"+strings.Repeat("on", boolInt(tc.yaml)), func(t *testing.T) {
			t.Setenv("GRIDCTL_EXPERIMENTAL_A2A", tc.env)
			s := a2aStack()
			s.Experimental["a2a"] = tc.yaml
			err := Validate(s)
			if tc.want {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "experimental.a2a")
			}
		})
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestValidate_A2AExclusiveSources(t *testing.T) {
	t.Setenv("GRIDCTL_EXPERIMENTAL_A2A", "true")
	for name, edit := range map[string]func(*MCPServer){
		"image":         func(s *MCPServer) { s.Image = "test" },
		"source":        func(s *MCPServer) { s.Source = &Source{Type: "local", Path: "."} },
		"url":           func(s *MCPServer) { s.URL = "https://example.com/mcp" },
		"command":       func(s *MCPServer) { s.Command = []string{"server"} },
		"ssh":           func(s *MCPServer) { s.SSH = &SSHConfig{Host: "host", User: "user"}; s.Command = []string{"server"} },
		"malformed ssh": func(s *MCPServer) { s.SSH = &SSHConfig{} },
		"openapi":       func(s *MCPServer) { s.OpenAPI = &OpenAPIConfig{Spec: "https://example.com/spec"} },
	} {
		t.Run(name, func(t *testing.T) {
			s := a2aStack()
			edit(&s.MCPServers[0])
			require.False(t, s.MCPServers[0].IsA2A())
			require.ErrorContains(t, Validate(s), "can only have one")
		})
	}
}

func TestA2AAuth_UnmarshalYAML(t *testing.T) {
	for _, value := range []string{"''", "null", "123", "{}", "session"} {
		var s MCPServer
		require.ErrorContains(t, yaml.Unmarshal([]byte("a2a: {card: 'https://example.com/card', auth: {type: bearer, token: '${var:TOKEN}', session_id: "+value+"}}"), &s), "session_id")
	}
	var s MCPServer
	require.ErrorContains(t, yaml.Unmarshal([]byte("a2a: {card: 'https://example.com/card', auth: {<<: {session_id: null}, type: bearer}}"), &s), "session_id")
	require.NoError(t, yaml.Unmarshal([]byte("a2a: {card: 'https://example.com/card', auth: {type: bearer, token: '${var:TOKEN}'}}"), &s))
	require.Equal(t, "${var:TOKEN}", s.A2A.Auth.Token)
}

func TestMCPServer_A2AClassificationAndEquality(t *testing.T) {
	s := a2aStack()
	require.True(t, s.MCPServers[0].IsA2A())
	require.False(t, s.NeedsContainerRuntime())
	require.Empty(t, s.ContainerWorkloads())
	require.Len(t, s.NonContainerWorkloads(), 1)
	require.Contains(t, s.NonContainerWorkloads()[0], "(a2a)")
	_, err := ResolveExecution(s.MCPServers[0])
	require.NoError(t, err)
	a, b := s.MCPServers[0], s.MCPServers[0]
	copy := *b.A2A
	b.A2A = &copy
	b.A2A.Dialect = "auto"
	b.A2A.Timeout = "5m"
	require.True(t, MCPServerEqual(a, b))
	b.A2A.Endpoint = "https://example.com/other"
	require.False(t, MCPServerEqual(a, b))
	require.Empty(t, a.A2A.Timeout)
	s.MCPServers = append(s.MCPServers, MCPServer{Name: "container", Image: "test"})
	require.True(t, s.NeedsContainerRuntime())
}
