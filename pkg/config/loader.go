package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadTopology reads and parses a topology file.
func LoadTopology(path string) (*Topology, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading topology file: %w", err)
	}

	var topology Topology
	if err := yaml.Unmarshal(data, &topology); err != nil {
		return nil, fmt.Errorf("parsing topology YAML: %w", err)
	}

	// Merge equipped_skills alias into uses for each agent
	mergeEquippedSkills(&topology)

	// Expand environment variables in string values
	expandEnvVars(&topology)

	// Apply defaults
	topology.SetDefaults()

	// Resolve relative paths based on topology file location
	basePath := filepath.Dir(path)
	resolveRelativePaths(&topology, basePath)

	// Validate the topology
	if err := Validate(&topology); err != nil {
		return nil, err
	}

	return &topology, nil
}

// expandEnvVars expands environment variables in the topology.
func expandEnvVars(t *Topology) {
	t.Name = os.ExpandEnv(t.Name)
	t.Network.Name = os.ExpandEnv(t.Network.Name)

	// Expand networks (advanced mode)
	for i := range t.Networks {
		t.Networks[i].Name = os.ExpandEnv(t.Networks[i].Name)
	}

	for i := range t.MCPServers {
		t.MCPServers[i].Name = os.ExpandEnv(t.MCPServers[i].Name)
		t.MCPServers[i].Image = os.ExpandEnv(t.MCPServers[i].Image)
		t.MCPServers[i].Network = os.ExpandEnv(t.MCPServers[i].Network)

		if t.MCPServers[i].Source != nil {
			t.MCPServers[i].Source.URL = os.ExpandEnv(t.MCPServers[i].Source.URL)
			t.MCPServers[i].Source.Path = os.ExpandEnv(t.MCPServers[i].Source.Path)
			t.MCPServers[i].Source.Ref = os.ExpandEnv(t.MCPServers[i].Source.Ref)
		}

		for k, v := range t.MCPServers[i].Env {
			t.MCPServers[i].Env[k] = os.ExpandEnv(v)
		}
		for k, v := range t.MCPServers[i].BuildArgs {
			t.MCPServers[i].BuildArgs[k] = os.ExpandEnv(v)
		}

		// Expand SSH config environment variables
		if t.MCPServers[i].SSH != nil {
			t.MCPServers[i].SSH.Host = os.ExpandEnv(t.MCPServers[i].SSH.Host)
			t.MCPServers[i].SSH.User = os.ExpandEnv(t.MCPServers[i].SSH.User)
			t.MCPServers[i].SSH.IdentityFile = os.ExpandEnv(t.MCPServers[i].SSH.IdentityFile)
		}
	}

	for i := range t.Resources {
		t.Resources[i].Name = os.ExpandEnv(t.Resources[i].Name)
		t.Resources[i].Image = os.ExpandEnv(t.Resources[i].Image)
		t.Resources[i].Network = os.ExpandEnv(t.Resources[i].Network)

		for k, v := range t.Resources[i].Env {
			t.Resources[i].Env[k] = os.ExpandEnv(v)
		}
	}

	for i := range t.Agents {
		t.Agents[i].Name = os.ExpandEnv(t.Agents[i].Name)
		t.Agents[i].Image = os.ExpandEnv(t.Agents[i].Image)
		t.Agents[i].Description = os.ExpandEnv(t.Agents[i].Description)
		t.Agents[i].Network = os.ExpandEnv(t.Agents[i].Network)

		if t.Agents[i].Source != nil {
			t.Agents[i].Source.URL = os.ExpandEnv(t.Agents[i].Source.URL)
			t.Agents[i].Source.Path = os.ExpandEnv(t.Agents[i].Source.Path)
			t.Agents[i].Source.Ref = os.ExpandEnv(t.Agents[i].Source.Ref)
		}

		for k, v := range t.Agents[i].Env {
			t.Agents[i].Env[k] = os.ExpandEnv(v)
		}
		for k, v := range t.Agents[i].BuildArgs {
			t.Agents[i].BuildArgs[k] = os.ExpandEnv(v)
		}
	}
}

// resolveRelativePaths resolves local source paths relative to the topology file.
func resolveRelativePaths(t *Topology, basePath string) {
	for i := range t.MCPServers {
		if t.MCPServers[i].Source != nil && t.MCPServers[i].Source.Type == "local" {
			if !filepath.IsAbs(t.MCPServers[i].Source.Path) {
				t.MCPServers[i].Source.Path = filepath.Join(basePath, t.MCPServers[i].Source.Path)
			}
		}

		// Resolve SSH identity file paths
		if t.MCPServers[i].SSH != nil && t.MCPServers[i].SSH.IdentityFile != "" {
			t.MCPServers[i].SSH.IdentityFile = expandTildeAndResolvePath(t.MCPServers[i].SSH.IdentityFile, basePath)
		}
	}

	for i := range t.Agents {
		if t.Agents[i].Source != nil && t.Agents[i].Source.Type == "local" {
			if !filepath.IsAbs(t.Agents[i].Source.Path) {
				t.Agents[i].Source.Path = filepath.Join(basePath, t.Agents[i].Source.Path)
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

// mergeEquippedSkills merges the equipped_skills YAML alias into uses.
// This allows users to use either field name in their topology files.
func mergeEquippedSkills(t *Topology) {
	for i := range t.Agents {
		if len(t.Agents[i].EquippedSkills) > 0 {
			// Merge without duplicates
			seen := make(map[string]bool)
			for _, u := range t.Agents[i].Uses {
				seen[u] = true
			}
			for _, s := range t.Agents[i].EquippedSkills {
				if !seen[s] {
					t.Agents[i].Uses = append(t.Agents[i].Uses, s)
					seen[s] = true
				}
			}
			// Clear the alias field after merging
			t.Agents[i].EquippedSkills = nil
		}
	}
}
