package config

import "testing"

// mockSetVault implements VaultSetLookup for injectSetSecrets tests.
type mockSetVault struct {
	secrets map[string]string
	sets    map[string][]VaultSecret
}

func (m *mockSetVault) Get(key string) (string, bool) {
	v, ok := m.secrets[key]
	return v, ok
}

func (m *mockSetVault) GetSetSecrets(setName string) []VaultSecret {
	return m.sets[setName]
}

// setRefs builds unscoped (fan-out) set references, the shorthand form.
func setRefs(names ...string) []SecretSetRef {
	refs := make([]SecretSetRef, 0, len(names))
	for _, n := range names {
		refs = append(refs, SecretSetRef{Name: n})
	}
	return refs
}

func TestInjectSetSecrets(t *testing.T) {
	devSet := map[string][]VaultSecret{
		"dev": {
			{Key: "API_KEY", Value: "sk-123"},
			{Key: "TOKEN", Value: "tok-456"},
		},
	}

	t.Run("injects into server and resource env", func(t *testing.T) {
		s := &Stack{
			Secrets:    &Secrets{Sets: setRefs("dev")},
			MCPServers: []MCPServer{{Name: "github"}},
			Resources:  []Resource{{Name: "db"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		for key, want := range map[string]string{"API_KEY": "sk-123", "TOKEN": "tok-456"} {
			if got := s.MCPServers[0].Env[key]; got != want {
				t.Errorf("server env[%s] = %q, want %q", key, got, want)
			}
			if got := s.Resources[0].Env[key]; got != want {
				t.Errorf("resource env[%s] = %q, want %q", key, got, want)
			}
		}
	})

	t.Run("explicit YAML env wins over injected value", func(t *testing.T) {
		s := &Stack{
			Secrets: &Secrets{Sets: setRefs("dev")},
			MCPServers: []MCPServer{{
				Name: "github",
				Env:  map[string]string{"API_KEY": "explicit"},
			}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if got := s.MCPServers[0].Env["API_KEY"]; got != "explicit" {
			t.Errorf("env[API_KEY] = %q, want explicit value preserved", got)
		}
		if got := s.MCPServers[0].Env["TOKEN"]; got != "tok-456" {
			t.Errorf("env[TOKEN] = %q, want tok-456", got)
		}
	})

	t.Run("unknown set is a no-op", func(t *testing.T) {
		s := &Stack{
			Secrets:    &Secrets{Sets: setRefs("missing")},
			MCPServers: []MCPServer{{Name: "github"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if len(s.MCPServers[0].Env) != 0 {
			t.Errorf("env = %v, want untouched (nil map never allocated with values)", s.MCPServers[0].Env)
		}
	})

	t.Run("does not touch the reference index", func(t *testing.T) {
		s := &Stack{
			Secrets:    &Secrets{Sets: setRefs("dev")},
			MCPServers: []MCPServer{{Name: "github"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if len(s.References) != 0 {
			t.Errorf("References = %v, want empty — injection is index-invisible by design", s.References)
		}
	})
}

func TestInjectSetSecrets_Scoping(t *testing.T) {
	devSet := map[string][]VaultSecret{
		"dev": {{Key: "API_KEY", Value: "sk-123"}},
		"db":  {{Key: "PGPASSWORD", Value: "pw"}},
	}

	t.Run("scoped set reaches only the named server", func(t *testing.T) {
		s := &Stack{
			Secrets: &Secrets{Sets: []SecretSetRef{
				{Name: "dev", Servers: []string{"github"}},
			}},
			MCPServers: []MCPServer{{Name: "github"}, {Name: "playwright"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if got := s.MCPServers[0].Env["API_KEY"]; got != "sk-123" {
			t.Errorf("github env[API_KEY] = %q, want sk-123", got)
		}
		if _, present := s.MCPServers[1].Env["API_KEY"]; present {
			t.Error("playwright received API_KEY; a scoped set must not reach unnamed servers")
		}
	})

	t.Run("server-scoped set reaches no resources", func(t *testing.T) {
		s := &Stack{
			Secrets: &Secrets{Sets: []SecretSetRef{
				{Name: "dev", Servers: []string{"github"}},
			}},
			MCPServers: []MCPServer{{Name: "github"}},
			Resources:  []Resource{{Name: "cache"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if _, present := s.Resources[0].Env["API_KEY"]; present {
			t.Error("resource received a set scoped to servers only")
		}
	})

	t.Run("resource scoping is independent of server scoping", func(t *testing.T) {
		s := &Stack{
			Secrets: &Secrets{Sets: []SecretSetRef{
				{Name: "db", Resources: []string{"postgres"}},
			}},
			MCPServers: []MCPServer{{Name: "github"}},
			Resources:  []Resource{{Name: "postgres"}, {Name: "cache"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if got := s.Resources[0].Env["PGPASSWORD"]; got != "pw" {
			t.Errorf("postgres env[PGPASSWORD] = %q, want pw", got)
		}
		if _, present := s.Resources[1].Env["PGPASSWORD"]; present {
			t.Error("cache received a set scoped to postgres")
		}
		if _, present := s.MCPServers[0].Env["PGPASSWORD"]; present {
			t.Error("server received a set scoped to resources only")
		}
	})

	t.Run("scoped and unscoped entries coexist", func(t *testing.T) {
		s := &Stack{
			Secrets: &Secrets{Sets: []SecretSetRef{
				{Name: "db", Servers: []string{"github"}},
				{Name: "dev"}, // unscoped: everything
			}},
			MCPServers: []MCPServer{{Name: "github"}, {Name: "playwright"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if got := s.MCPServers[0].Env["PGPASSWORD"]; got != "pw" {
			t.Errorf("github env[PGPASSWORD] = %q, want pw", got)
		}
		if got := s.MCPServers[1].Env["API_KEY"]; got != "sk-123" {
			t.Errorf("playwright env[API_KEY] = %q, want the unscoped set to still fan out", got)
		}
		if _, present := s.MCPServers[1].Env["PGPASSWORD"]; present {
			t.Error("playwright received the server-scoped db set")
		}
	})

	t.Run("scoped name matching nothing injects nowhere", func(t *testing.T) {
		s := &Stack{
			Secrets: &Secrets{Sets: []SecretSetRef{
				{Name: "dev", Servers: []string{"typo"}},
			}},
			MCPServers: []MCPServer{{Name: "github"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if len(s.MCPServers[0].Env) != 0 {
			t.Errorf("env = %v, want untouched", s.MCPServers[0].Env)
		}
	})

	t.Run("explicit YAML env still wins over a scoped set", func(t *testing.T) {
		s := &Stack{
			Secrets: &Secrets{Sets: []SecretSetRef{
				{Name: "dev", Servers: []string{"github"}},
			}},
			MCPServers: []MCPServer{{
				Name: "github",
				Env:  map[string]string{"API_KEY": "explicit"},
			}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if got := s.MCPServers[0].Env["API_KEY"]; got != "explicit" {
			t.Errorf("env[API_KEY] = %q, want explicit value preserved", got)
		}
	})
}
