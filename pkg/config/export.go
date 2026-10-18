package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ExportNotice describes the bounded policy, not a guarantee about arbitrary text.
const ExportNotice = "Export preserves authored references without resolving values. Review authored literals, including mixed reference/literal strings, before sharing."

// ExportConfigFilesNotice is appended when the export contains file-backed configs.
const ExportConfigFilesNotice = "Referenced config files are not bundled; copy them alongside the exported stack."

// ExportOverrideNotice is appended when an override exists and was not merged.
func ExportOverrideNotice(path string) string {
	return "an override file exists at " + path + "; pass --with-override to include it"
}

// ExportNotices returns the stderr and REST notice for an exported stack.
func ExportNotices(stack *Stack) string {
	notice := ExportNotice
	if stackReferencesConfigFiles(stack) {
		notice += "\n" + ExportConfigFilesNotice
	}
	if stack != nil && stack.DiscoveredOverride != "" {
		notice += "\n" + ExportOverrideNotice(stack.DiscoveredOverride)
	}
	return notice
}

func stackReferencesConfigFiles(stack *Stack) bool {
	if stack == nil {
		return false
	}
	for _, srv := range stack.MCPServers {
		for _, cfg := range srv.Configs {
			if cfg.File != "" {
				return true
			}
		}
	}
	return false
}

// ExportStack reads an unresolved export projection and returns every source path
// for destination collision checks. It does not access stores or runtime values.
// A discovered override is not merged; ExportNotices names it.
func ExportStack(ctx context.Context, path string) (*Stack, []string, error) {
	return exportStack(ctx, path, "")
}

// ExportWithOverride merges overridePath onto the unresolved projection after
// the extends read and before credential checks. It does not route through
// LoadStack, so authored references stay unresolved.
func ExportWithOverride(ctx context.Context, path, overridePath string) (*Stack, []string, error) {
	if overridePath == "" {
		return nil, nil, fmt.Errorf("override: path is required")
	}
	if _, err := os.Stat(overridePath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("override: %s: file does not exist", overridePath)
		}
		return nil, nil, fmt.Errorf("override: %s: %w", overridePath, err)
	}
	return exportStack(ctx, path, overridePath)
}

func exportStack(ctx context.Context, path, overridePath string) (*Stack, []string, error) {
	var sources []string
	stack, err := readExportStack(ctx, path, &sources)
	if err != nil {
		return nil, nil, err
	}
	if overridePath != "" {
		if err := mergeOverrideFile(stack, overridePath, false); err != nil {
			return nil, nil, err
		}
	} else if discovered, found, discErr := DiscoverOverride(path, OverrideSelection{}); discErr != nil {
		return nil, nil, discErr
	} else if found {
		stack.DiscoveredOverride = discovered
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
	stack, err := DecodeStack(data)
	if err != nil {
		location := exportSyntaxLocation(err)
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
			for j := range srv.Configs {
				if srv.Configs[j].File != "" {
					paths = append(paths, &srv.Configs[j].File)
				}
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
		mergeStacks(stack, parent)
		stack.Extends = ""
	}
	return stack, nil
}

func exportSyntaxLocation(err error) string {
	var ve ValidationErrors
	if errors.As(err, &ve) {
		for _, item := range ve {
			if match := exportErrorLine.FindStringSubmatch(item.Message); match != nil {
				return " at line " + match[1]
			}
		}
	}
	if match := exportErrorLine.FindStringSubmatch(err.Error()); match != nil {
		return " at line " + match[1]
	}
	return ""
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
	checkHeaders := func(path string, headers map[string]string) error {
		names := make([]string, 0, len(headers))
		for name := range headers {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			if err := check(fmt.Sprintf("%s[%d]", path, i), headers[name]); err != nil {
				return err
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
		if err := checkHeaders(prefix+".headers", srv.Headers); err != nil {
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
