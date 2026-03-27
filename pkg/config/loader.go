package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// loadConfig holds options for LoadStack.
type loadConfig struct {
	vault    VaultLookup
	vaultSet VaultSetLookup
}

// LoadOption configures LoadStack behavior.
type LoadOption func(*loadConfig)

// WithVault enables ${vault:KEY} resolution during stack loading.
func WithVault(v VaultLookup) LoadOption {
	return func(c *loadConfig) { c.vault = v }
}

// WithVaultSets enables secrets.sets injection during stack loading.
func WithVaultSets(v VaultSetLookup) LoadOption {
	return func(c *loadConfig) { c.vaultSet = v }
}

// LoadStack reads and parses a stack file.
func LoadStack(path string, opts ...LoadOption) (*Stack, error) {
	var cfg loadConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading stack file: %w", err)
	}

	var stack Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return nil, fmt.Errorf("parsing stack YAML: %w", err)
	}


	// Build resolver
	var resolve Resolver
	if cfg.vault != nil {
		resolve = VaultResolver(cfg.vault)
	} else {
		resolve = EnvResolver()
	}

	// Expand variable references in string values
	unresolved, emptyVars := expandStackVars(&stack, resolve)

	// Fail on unresolved vault references only if a vault was provided
	if cfg.vault != nil && len(unresolved) > 0 {
		msg := fmt.Sprintf("missing vault secret(s): %s", strings.Join(unresolved, ", "))
		msg += "\n  To fix: gridctl vault set <KEY>"
		return nil, fmt.Errorf("%s", msg)
	}

	// Hint about empty env vars that could use vault
	if cfg.vault == nil {
		for _, v := range emptyVars {
			slog.Info("hint: "+v+" resolved to empty — use 'gridctl vault set "+v+"' to store it securely", "var", v)
		}
	}

	// Apply defaults
	stack.SetDefaults()

	// Resolve relative paths based on stack file location
	basePath := filepath.Dir(path)
	resolveRelativePaths(&stack, basePath)

	// Validate the stack
	if err := Validate(&stack); err != nil {
		return nil, err
	}

	// Inject variable set secrets into container env
	if stack.Secrets != nil && len(stack.Secrets.Sets) > 0 && cfg.vaultSet != nil {
		injectSetSecrets(&stack, cfg.vaultSet)
	}

	return &stack, nil
}

// injectSetSecrets resolves secrets from variable sets and injects them into container env.
// Explicit env values in YAML take precedence over set-injected values.
func injectSetSecrets(s *Stack, vault VaultSetLookup) {
	// Collect all secrets from referenced sets
	setSecrets := make(map[string]string)
	for _, setName := range s.Secrets.Sets {
		for _, sec := range vault.GetSetSecrets(setName) {
			setSecrets[sec.Key] = sec.Value
		}
	}

	if len(setSecrets) == 0 {
		return
	}

	// Inject into MCP servers
	for i := range s.MCPServers {
		if s.MCPServers[i].Env == nil {
			s.MCPServers[i].Env = make(map[string]string)
		}
		for k, v := range setSecrets {
			if _, exists := s.MCPServers[i].Env[k]; !exists {
				s.MCPServers[i].Env[k] = v
			}
		}
	}

	// Inject into resources
	for i := range s.Resources {
		if s.Resources[i].Env == nil {
			s.Resources[i].Env = make(map[string]string)
		}
		for k, v := range setSecrets {
			if _, exists := s.Resources[i].Env[k]; !exists {
				s.Resources[i].Env[k] = v
			}
		}
	}
}

// expandStackVars expands variable references in all stack string fields using the
// unified ExpandString function. Returns unresolved vault references and empty env vars.
func expandStackVars(s *Stack, resolve Resolver) (unresolvedVault []string, emptyEnvVars []string) {
	expand := func(val string) string {
		result, unresolved, empty := ExpandString(val, resolve)
		unresolvedVault = append(unresolvedVault, unresolved...)
		emptyEnvVars = append(emptyEnvVars, empty...)
		return result
	}

	s.Name = expand(s.Name)

	if s.Gateway != nil {
		for i := range s.Gateway.AllowedOrigins {
			s.Gateway.AllowedOrigins[i] = expand(s.Gateway.AllowedOrigins[i])
		}
		if s.Gateway.Auth != nil {
			s.Gateway.Auth.Token = expand(s.Gateway.Auth.Token)
		}
	}

	s.Network.Name = expand(s.Network.Name)

	for i := range s.Networks {
		s.Networks[i].Name = expand(s.Networks[i].Name)
	}

	for i := range s.MCPServers {
		s.MCPServers[i].Name = expand(s.MCPServers[i].Name)
		s.MCPServers[i].Image = expand(s.MCPServers[i].Image)
		s.MCPServers[i].URL = expand(s.MCPServers[i].URL)
		s.MCPServers[i].Network = expand(s.MCPServers[i].Network)

		for j := range s.MCPServers[i].Command {
			s.MCPServers[i].Command[j] = expand(s.MCPServers[i].Command[j])
		}

		if s.MCPServers[i].Source != nil {
			s.MCPServers[i].Source.URL = expand(s.MCPServers[i].Source.URL)
			s.MCPServers[i].Source.Path = expand(s.MCPServers[i].Source.Path)
			s.MCPServers[i].Source.Ref = expand(s.MCPServers[i].Source.Ref)
		}

		for k, v := range s.MCPServers[i].Env {
			s.MCPServers[i].Env[k] = expand(v)
		}
		for k, v := range s.MCPServers[i].BuildArgs {
			s.MCPServers[i].BuildArgs[k] = expand(v)
		}

		if s.MCPServers[i].SSH != nil {
			s.MCPServers[i].SSH.Host = expand(s.MCPServers[i].SSH.Host)
			s.MCPServers[i].SSH.User = expand(s.MCPServers[i].SSH.User)
			s.MCPServers[i].SSH.IdentityFile = expand(s.MCPServers[i].SSH.IdentityFile)
		}

		if s.MCPServers[i].OpenAPI != nil {
			s.MCPServers[i].OpenAPI.Spec = expand(s.MCPServers[i].OpenAPI.Spec)
			s.MCPServers[i].OpenAPI.BaseURL = expand(s.MCPServers[i].OpenAPI.BaseURL)
		}
	}

	for i := range s.Resources {
		s.Resources[i].Name = expand(s.Resources[i].Name)
		s.Resources[i].Image = expand(s.Resources[i].Image)
		s.Resources[i].Network = expand(s.Resources[i].Network)

		for k, v := range s.Resources[i].Env {
			s.Resources[i].Env[k] = expand(v)
		}
	}

	return unresolvedVault, emptyEnvVars
}

// resolveRelativePaths resolves local source paths relative to the stack file.
func resolveRelativePaths(s *Stack, basePath string) {
	for i := range s.MCPServers {
		if s.MCPServers[i].Source != nil && s.MCPServers[i].Source.Type == "local" {
			if !filepath.IsAbs(s.MCPServers[i].Source.Path) {
				s.MCPServers[i].Source.Path = filepath.Join(basePath, s.MCPServers[i].Source.Path)
			}
		}

		// Resolve SSH identity file paths
		if s.MCPServers[i].SSH != nil && s.MCPServers[i].SSH.IdentityFile != "" {
			s.MCPServers[i].SSH.IdentityFile = expandTildeAndResolvePath(s.MCPServers[i].SSH.IdentityFile, basePath)
		}

		// Resolve OpenAPI spec paths (if not a URL)
		if s.MCPServers[i].OpenAPI != nil && s.MCPServers[i].OpenAPI.Spec != "" {
			if !isURL(s.MCPServers[i].OpenAPI.Spec) {
				s.MCPServers[i].OpenAPI.Spec = expandTildeAndResolvePath(s.MCPServers[i].OpenAPI.Spec, basePath)
			}
		}
	}

}

// expandTildeAndResolvePath expands ~ to home directory and resolves relative paths.
func expandTildeAndResolvePath(path, basePath string) string {
	// Expand ~ to home directory
	if len(path) > 0 && path[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			if len(path) == 1 {
				path = home
			} else if path[1] == '/' || path[1] == filepath.Separator {
				path = filepath.Join(home, path[2:])
			}
		}
	}

	// Resolve relative paths
	if !filepath.IsAbs(path) {
		path = filepath.Join(basePath, path)
	}

	return path
}

// isURL checks if a string looks like a URL (http:// or https://).
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

