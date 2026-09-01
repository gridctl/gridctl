package config

import "testing"

func TestSourceEqual_AllFields(t *testing.T) {
	base := &Source{Type: "git", URL: "https://example.com/repo", Ref: "main", Path: "subdir", Dockerfile: "Containerfile",
		Runtime: "python", Python: "3.12", Extras: []string{"http"}, With: []string{"httpx>=0.27"}, Packages: []string{"curl"},
		Auth: &SourceAuth{Method: "ssh-key", CredentialRef: "${var:GIT_KEY}", SSHUser: "git", SSHKeyPath: "/key"}}
	if !SourceEqual(base, base) {
		t.Fatal("identical sources must be equal")
	}

	tests := []struct {
		name   string
		mutate func(*Source)
	}{
		{"type", func(s *Source) { s.Type = "local" }},
		{"url", func(s *Source) { s.URL += "-other" }},
		{"ref", func(s *Source) { s.Ref = "v2" }},
		{"path", func(s *Source) { s.Path = "other" }},
		{"dockerfile", func(s *Source) { s.Dockerfile = "Dockerfile" }},
		{"runtime", func(s *Source) { s.Runtime = "" }},
		{"python", func(s *Source) { s.Python = "3.11" }},
		{"extras", func(s *Source) { s.Extras = []string{"cli"} }},
		{"with", func(s *Source) { s.With = []string{"anyio"} }},
		{"packages", func(s *Source) { s.Packages = []string{"git"} }},
		{"auth method", func(s *Source) { s.Auth.Method = "token" }},
		{"credential", func(s *Source) { s.Auth.CredentialRef = "${var:OTHER}" }},
		{"ssh user", func(s *Source) { s.Auth.SSHUser = "builder" }},
		{"ssh key", func(s *Source) { s.Auth.SSHKeyPath = "/other" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := *base
			auth := *base.Auth
			changed.Auth = &auth
			tt.mutate(&changed)
			if SourceEqual(base, &changed) {
				t.Fatal("changed source must not be equal")
			}
		})
	}
}

func TestSourceEqual_PythonDockerfileOmissionIsSignificant(t *testing.T) {
	omitted := &Source{Type: "git", URL: "https://example.com/repo", Runtime: "python"}
	explicit := &Source{Type: "git", URL: "https://example.com/repo", Runtime: "python", Dockerfile: "Dockerfile"}
	if SourceEqual(omitted, explicit) {
		t.Fatal("generated and explicit Dockerfile strategies must differ")
	}
}

func TestMCPServerEqual_NormalizesPythonDefaults(t *testing.T) {
	omitted := MCPServer{Name: "package", Source: &Source{Type: "pypi", Package: "demo", Ref: "1.0"}}
	defaulted := omitted
	defaulted.Source = &Source{Type: "pypi", Package: "demo", Ref: "1.0", Runtime: "python"}
	defaulted.Transport = "stdio"
	if !MCPServerEqual(omitted, defaulted) {
		t.Fatal("omitted and explicit Python defaults must be equal")
	}
}

func TestMCPServerEqual_DoesNotDefaultCustomPythonDockerfileToStdio(t *testing.T) {
	omitted := MCPServer{Name: "custom", Source: &Source{Type: "local", Path: ".", Runtime: "python", Dockerfile: "Dockerfile"}}
	stdio := omitted
	stdio.Transport = "stdio"
	if MCPServerEqual(omitted, stdio) {
		t.Fatal("custom Dockerfile transport omission must retain the legacy HTTP default")
	}
}

func TestSourceEqual_NormalizesLegacyDefaults(t *testing.T) {
	omitted := &Source{Type: "git", URL: "https://example.com/repo"}
	defaulted := &Source{Type: "git", URL: "https://example.com/repo", Ref: "main", Dockerfile: "Dockerfile"}
	if !SourceEqual(omitted, defaulted) {
		t.Fatal("omitted and explicit legacy defaults must be equal")
	}
}

func TestMCPServerEqual_EffectiveCollectionsAndDurations(t *testing.T) {
	a := MCPServer{Name: "server", Image: "image", Autoscale: &AutoscaleConfig{Min: 1, Max: 2, TargetInFlight: 1}}
	b := a
	b.Command = []string{}
	b.Env = map[string]string{}
	b.BuildArgs = map[string]string{}
	b.Tools = []string{}
	b.Volumes = []string{}
	b.Autoscale = &AutoscaleConfig{Min: 1, Max: 2, TargetInFlight: 1, ScaleUpAfter: "30000ms", ScaleDownAfter: "300s"}
	if !MCPServerEqual(a, b) {
		t.Fatal("effectively identical servers must be equal")
	}
}

func TestMCPServerEqual_DetectsBuildAndReplicaChanges(t *testing.T) {
	base := MCPServer{Name: "server", Source: &Source{Type: "git", URL: "https://example.com/repo"},
		BuildArgs: map[string]string{"VERSION": "1"}, Replicas: 2, ReplicaPolicy: "round-robin"}
	tests := []MCPServer{
		{Name: "server", Source: &Source{Type: "git", URL: "https://example.com/other"}, BuildArgs: base.BuildArgs, Replicas: 2, ReplicaPolicy: "round-robin"},
		{Name: "server", Source: base.Source, BuildArgs: map[string]string{"VERSION": "2"}, Replicas: 2, ReplicaPolicy: "round-robin"},
		{Name: "server", Source: base.Source, BuildArgs: base.BuildArgs, Replicas: 3, ReplicaPolicy: "round-robin"},
		{Name: "server", Source: base.Source, BuildArgs: base.BuildArgs, Replicas: 2, ReplicaPolicy: "least-connections"},
		{Name: "server", Source: base.Source, BuildArgs: base.BuildArgs, Volumes: []string{"data:/data:ro"}, Replicas: 2, ReplicaPolicy: "round-robin"},
	}
	for i, changed := range tests {
		if MCPServerEqual(base, changed) {
			t.Fatalf("change %d was not detected", i)
		}
	}
}
