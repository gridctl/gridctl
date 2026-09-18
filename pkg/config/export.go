package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExportNotice describes the bounded policy, not a guarantee about arbitrary text.
const ExportNotice = "Export preserves authored references without resolving values. Review authored literals, including mixed reference/literal strings, before sharing."

// ExportStack reads an unresolved export projection and returns every source path
// for destination collision checks. It does not access stores or runtime values.
func ExportStack(ctx context.Context, path string) (*Stack, []string, error) {
	var sources []string
	stack, err := readExportStack(ctx, path, &sources)
	if err != nil {
		return nil, nil, err
	}
	if err := checkExportCredentials(stack); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return stack, sources, nil
}

var exportErrorLine = regexp.MustCompile(`\bline ([0-9]+)\b`)

func readExportStack(ctx context.Context, path string, sources *[]string) (*Stack, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(*sources) > maxExtendsDepth {
		return nil, fmt.Errorf("export: inheritance depth exceeded; simplify extends")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("export: cannot locate source file")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("export: cannot read source file at inheritance index %d", len(*sources))
	}
	for _, source := range *sources {
		previous, statErr := os.Stat(source)
		if statErr != nil || os.SameFile(info, previous) {
			return nil, fmt.Errorf("export: circular or unreadable inheritance; check extends")
		}
	}
	*sources = append(*sources, abs)
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("export: cannot read source file at inheritance index %d", len(*sources)-1)
	}
	var stack Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		location := ""
		if match := exportErrorLine.FindStringSubmatch(err.Error()); match != nil {
			location = " at line " + match[1]
		}
		return nil, fmt.Errorf("export: invalid stack syntax%s (inheritance index %d); check source locally", location, len(*sources)-1)
	}
	if stack.Extends != "" {
		parentPath := stack.Extends
		if !filepath.IsAbs(parentPath) {
			parentPath = filepath.Join(filepath.Dir(abs), parentPath)
		}
		parent, err := readExportStack(ctx, parentPath, sources)
		if err != nil {
			return nil, err
		}
		if err := checkDeclarationConflicts(stack.Variables, parent.Variables); err != nil {
			return nil, fmt.Errorf("export: conflicting inherited variable declarations; check source locally")
		}
		// Runtime anchors parent paths before merging. Expressions and tilde
		// paths cannot be anchored without changing their recipient semantics.
		for i := range parent.MCPServers {
			srv := &parent.MCPServers[i]
			shadowed := false
			for _, child := range stack.MCPServers {
				if child.Name == srv.Name {
					shadowed = true
				}
			}
			if shadowed {
				continue
			}
			var paths []*string
			if srv.Source != nil {
				if srv.Source.Type == "local" {
					if srv.Source.Path == "" {
						return nil, fmt.Errorf("export: inherited mcp-servers[%d].source.path is empty and cannot retain its anchor; declare the workload in the child stack", i)
					}
					paths = append(paths, &srv.Source.Path)
				}
				if srv.Source.Auth != nil {
					paths = append(paths, &srv.Source.Auth.SSHKeyPath)
				}
			}
			if srv.SSH != nil {
				paths = append(paths, &srv.SSH.IdentityFile, &srv.SSH.KnownHostsFile)
			}
			if srv.OpenAPI != nil && !isURL(srv.OpenAPI.Spec) {
				paths = append(paths, &srv.OpenAPI.Spec)
			}
			for _, p := range paths {
				if *p == "" || filepath.IsAbs(*p) {
					continue
				}
				if expandRegex.MatchString(*p) || strings.HasPrefix(*p, "~") {
					return nil, fmt.Errorf("export: inherited mcp-servers[%d] path cannot retain its anchor; declare the workload in the child stack", i)
				}
				*p = filepath.Join(filepath.Dir(parentPath), *p)
			}
		}
		mergeStacks(&stack, parent)
		stack.Extends = ""
	}
	return &stack, nil
}

func checkExportCredentials(stack *Stack) error {
	check := func(path, value string) error {
		if value == "" {
			return nil
		}
		matches := expandRegex.FindAllStringSubmatch(value, -1)
		if len(matches) == 0 {
			return fmt.Errorf("export: %s: recognized sensitive field contains an inline literal; use an authored variable reference", path)
		}
		for _, match := range matches {
			// Expansion is single-pass: operands are literal even when they
			// themselves contain dollar signs.
			if match[3] != "" && match[4] != "" {
				return fmt.Errorf("export: %s: sensitive default/replacement operand; remove the operand and supply the variable separately", path)
			}
		}
		return nil
	}
	checkEnv := func(path string, env map[string]string) error {
		keys := make([]string, 0, len(env))
		for key := range env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i, key := range keys {
			for _, part := range []string{"PASSWORD", "SECRET", "TOKEN", "API_KEY", "APIKEY", "PRIVATE_KEY", "ACCESS_KEY", "AUTH", "CREDENTIAL"} {
				if strings.Contains(strings.ToUpper(key), part) {
					if err := check(fmt.Sprintf("%s[%d]", path, i), env[key]); err != nil {
						return err
					}
					break
				}
			}
		}
		return nil
	}
	if g := stack.Gateway; g != nil {
		if g.Auth != nil {
			if err := check("gateway.auth.token", g.Auth.Token); err != nil {
				return err
			}
		}
		if err := check("gateway.tokenizer_api_key", g.TokenizerAPIKey); err != nil {
			return err
		}
	}
	for i, srv := range stack.MCPServers {
		prefix := fmt.Sprintf("mcp-servers[%d]", i)
		if srv.A2A != nil && srv.A2A.Auth != nil {
			if err := check(prefix+".a2a.auth.token", srv.A2A.Auth.Token); err != nil {
				return err
			}
		}
		if srv.Auth != nil {
			for _, field := range []struct{ name, value string }{{"token", srv.Auth.Token}, {"value", srv.Auth.Value}, {"client_secret", srv.Auth.ClientSecret}} {
				if err := check(prefix+".auth."+field.name, field.value); err != nil {
					return err
				}
			}
		}
		if srv.Source != nil && srv.Source.Auth != nil {
			if err := check(prefix+".source.auth.credential_ref", srv.Source.Auth.CredentialRef); err != nil {
				return err
			}
		}
		if err := checkEnv(prefix+".env", srv.Env); err != nil {
			return err
		}
	}
	for i, res := range stack.Resources {
		if err := checkEnv(fmt.Sprintf("resources[%d].env", i), res.Env); err != nil {
			return err
		}
	}
	return nil
}
