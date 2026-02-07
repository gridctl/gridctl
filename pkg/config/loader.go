package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadStack reads and parses a stack file.
func LoadStack(path string) (*Stack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading stack file: %w", err)
	}

	var stack Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return nil, fmt.Errorf("parsing stack YAML: %w", err)
	}

	// Merge equipped_skills alias into uses for each agent
	mergeEquippedSkills(&stack)

	// Expand environment variables in string values
	expandEnvVars(&stack)

	// Apply defaults
	stack.SetDefaults()

	// Resolve relative paths based on stack file location
	basePath := filepath.Dir(path)
	resolveRelativePaths(&stack, basePath)

	// Validate the stack
	if err := Validate(&stack); err != nil {
		return nil, err
	}

	return &stack, nil
}

// expandEnvVars expands environment variables in the stack.
func expandEnvVars(s *Stack) {
	s.Name = os.ExpandEnv(s.Name)

	if s.Gateway != nil {
		for i := range s.Gateway.AllowedOrigins {
			s.Gateway.AllowedOrigins[i] = os.ExpandEnv(s.Gateway.AllowedOrigins[i])
		}
		if s.Gateway.Auth != nil {
			s.Gateway.Auth.Token = os.ExpandEnv(s.Gateway.Auth.Token)
		}
	}

	s.Network.Name = os.ExpandEnv(s.Network.Name)

	// Expand networks (advanced mode)
	for i := range s.Networks {
		s.Networks[i].Name = os.ExpandEnv(s.Networks[i].Name)
	}

	for i := range s.MCPServers {
		s.MCPServers[i].Name = os.ExpandEnv(s.MCPServers[i].Name)
		s.MCPServers[i].Image = os.ExpandEnv(s.MCPServers[i].Image)
		s.MCPServers[i].URL = os.ExpandEnv(s.MCPServers[i].URL)
		s.MCPServers[i].Network = os.ExpandEnv(s.MCPServers[i].Network)

		// Expand command arguments (for local process servers using env vars in URLs)
		for j := range s.MCPServers[i].Command {
			s.MCPServers[i].Command[j] = os.ExpandEnv(s.MCPServers[i].Command[j])
		}

		if s.MCPServers[i].Source != nil {
			s.MCPServers[i].Source.URL = os.ExpandEnv(s.MCPServers[i].Source.URL)
			s.MCPServers[i].Source.Path = os.ExpandEnv(s.MCPServers[i].Source.Path)
			s.MCPServers[i].Source.Ref = os.ExpandEnv(s.MCPServers[i].Source.Ref)
		}

		for k, v := range s.MCPServers[i].Env {
			s.MCPServers[i].Env[k] = os.ExpandEnv(v)
		}
		for k, v := range s.MCPServers[i].BuildArgs {
			s.MCPServers[i].BuildArgs[k] = os.ExpandEnv(v)
		}

		// Expand SSH config environment variables
		if s.MCPServers[i].SSH != nil {
			s.MCPServers[i].SSH.Host = os.ExpandEnv(s.MCPServers[i].SSH.Host)
			s.MCPServers[i].SSH.User = os.ExpandEnv(s.MCPServers[i].SSH.User)
			s.MCPServers[i].SSH.IdentityFile = os.ExpandEnv(s.MCPServers[i].SSH.IdentityFile)
		}

		// Expand OpenAPI config environment variables
		if s.MCPServers[i].OpenAPI != nil {
			s.MCPServers[i].OpenAPI.Spec = os.ExpandEnv(s.MCPServers[i].OpenAPI.Spec)
			s.MCPServers[i].OpenAPI.BaseURL = os.ExpandEnv(s.MCPServers[i].OpenAPI.BaseURL)
		}
	}

	for i := range s.Resources {
		s.Resources[i].Name = os.ExpandEnv(s.Resources[i].Name)
		s.Resources[i].Image = os.ExpandEnv(s.Resources[i].Image)
		s.Resources[i].Network = os.ExpandEnv(s.Resources[i].Network)

		for k, v := range s.Resources[i].Env {
			s.Resources[i].Env[k] = os.ExpandEnv(v)
		}
	}

	for i := range s.A2AAgents {
		s.A2AAgents[i].Name = os.ExpandEnv(s.A2AAgents[i].Name)
		s.A2AAgents[i].URL = os.ExpandEnv(s.A2AAgents[i].URL)
	}

	for i := range s.Agents {
		s.Agents[i].Name = os.ExpandEnv(s.Agents[i].Name)
		s.Agents[i].Image = os.ExpandEnv(s.Agents[i].Image)
		s.Agents[i].Description = os.ExpandEnv(s.Agents[i].Description)
		s.Agents[i].Network = os.ExpandEnv(s.Agents[i].Network)

		for j := range s.Agents[i].Command {
			s.Agents[i].Command[j] = os.ExpandEnv(s.Agents[i].Command[j])
		}

		if s.Agents[i].Source != nil {
			s.Agents[i].Source.URL = os.ExpandEnv(s.Agents[i].Source.URL)
			s.Agents[i].Source.Path = os.ExpandEnv(s.Agents[i].Source.Path)
			s.Agents[i].Source.Ref = os.ExpandEnv(s.Agents[i].Source.Ref)
		}

		for k, v := range s.Agents[i].Env {
			s.Agents[i].Env[k] = os.ExpandEnv(v)
		}
		for k, v := range s.Agents[i].BuildArgs {
			s.Agents[i].BuildArgs[k] = os.ExpandEnv(v)
		}
	}
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

	for i := range s.Agents {
		if s.Agents[i].Source != nil && s.Agents[i].Source.Type == "local" {
			if !filepath.IsAbs(s.Agents[i].Source.Path) {
				s.Agents[i].Source.Path = filepath.Join(basePath, s.Agents[i].Source.Path)
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

// mergeEquippedSkills merges the equipped_skills YAML alias into uses.
// This allows users to use either field name in their stack files.
func mergeEquippedSkills(s *Stack) {
	for i := range s.Agents {
		if len(s.Agents[i].EquippedSkills) > 0 {
			// Merge without duplicates (based on server name)
			seen := make(map[string]bool)
			for _, u := range s.Agents[i].Uses {
				seen[u.Server] = true
			}
			for _, skill := range s.Agents[i].EquippedSkills {
				if !seen[skill.Server] {
					s.Agents[i].Uses = append(s.Agents[i].Uses, skill)
					seen[skill.Server] = true
				}
			}
			// Clear the alias field after merging
			s.Agents[i].EquippedSkills = nil
		}
	}
}
