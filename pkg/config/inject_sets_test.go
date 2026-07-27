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

func TestInjectSetSecrets(t *testing.T) {
	devSet := map[string][]VaultSecret{
		"dev": {
			{Key: "API_KEY", Value: "sk-123"},
			{Key: "TOKEN", Value: "tok-456"},
		},
	}

	t.Run("injects into server and resource env", func(t *testing.T) {
		s := &Stack{
			Secrets:    &Secrets{Sets: []string{"dev"}},
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
			Secrets: &Secrets{Sets: []string{"dev"}},
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
			Secrets:    &Secrets{Sets: []string{"missing"}},
			MCPServers: []MCPServer{{Name: "github"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if len(s.MCPServers[0].Env) != 0 {
			t.Errorf("env = %v, want untouched (nil map never allocated with values)", s.MCPServers[0].Env)
		}
	})

	t.Run("does not touch the reference index", func(t *testing.T) {
		s := &Stack{
			Secrets:    &Secrets{Sets: []string{"dev"}},
			MCPServers: []MCPServer{{Name: "github"}},
		}
		injectSetSecrets(s, &mockSetVault{sets: devSet})

		if len(s.References) != 0 {
			t.Errorf("References = %v, want empty — injection is index-invisible by design", s.References)
		}
	})
}
