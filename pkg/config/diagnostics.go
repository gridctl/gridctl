package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// ParseStackIndex parses typed stack YAML, resolves extends, and builds a
// value-free reference index without environment or store expansion.
func ParseStackIndex(ctx context.Context, path string) (*Stack, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving stack path: %w", err)
	}
	return parseStackIndex(ctx, abs, map[string]bool{}, 0)
}

func parseStackIndex(ctx context.Context, path string, visited map[string]bool, depth int) (*Stack, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > maxExtendsDepth {
		return nil, fmt.Errorf("extends: maximum inheritance depth (%d) exceeded", maxExtendsDepth)
	}
	if visited[path] {
		return nil, fmt.Errorf("extends: circular dependency detected at %s", path)
	}
	visited[path] = true
	defer delete(visited, path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading stack file: %w", err)
	}
	var child Stack
	if err := yaml.Unmarshal(data, &child); err != nil {
		return nil, fmt.Errorf("parsing stack YAML: %w", err)
	}
	var indexCopy Stack
	if err := yaml.Unmarshal(data, &indexCopy); err != nil {
		return nil, fmt.Errorf("parsing stack YAML for reference index: %w", err)
	}
	_, _, problems := expandStackVarsResolved(&indexCopy, func(string, bool) ResolutionResult { return ResolutionResult{Verdict: ResolutionUnset} })
	if len(problems) > 0 {
		return nil, problems[0]
	}
	child.References = indexCopy.References
	for key, consumers := range child.References {
		for i := range consumers {
			consumers[i].Source = path
		}
		child.References[key] = consumers
	}
	if child.Extends == "" {
		return &child, nil
	}
	parentPath := child.Extends
	if !filepath.IsAbs(parentPath) {
		parentPath = filepath.Join(filepath.Dir(path), parentPath)
	}
	parentPath, err = filepath.Abs(parentPath)
	if err != nil {
		return nil, err
	}
	parent, err := parseStackIndex(ctx, parentPath, visited, depth+1)
	if err != nil {
		return nil, err
	}
	if err := checkDeclarationConflicts(child.Variables, parent.Variables); err != nil {
		return nil, err
	}
	childRefs := child.References
	mergeStacks(&child, parent)
	child.Extends = ""
	for key, consumers := range parent.References {
		childRefs[key] = append(childRefs[key], consumers...)
	}
	child.References = childRefs
	return &child, nil
}

func checkDeclarationConflicts(child, parent map[string]VariableDeclaration) error {
	for key, c := range child {
		p, ok := parent[key]
		if !ok {
			continue
		}
		if c.Type != "" && p.Type != "" && c.Type != p.Type {
			return fmt.Errorf("variable %s has conflicting declared types %q and %q", key, p.Type, c.Type)
		}
		if c.Secret != nil && p.Secret != nil && *c.Secret != *p.Secret {
			return fmt.Errorf("variable %s has conflicting sensitivity declarations", key)
		}
	}
	return nil
}

// DeclarationDiagnostic is one value-free advisory result.
type DeclarationDiagnostic struct {
	Key       string `json:"key"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Consumers int    `json:"consumers"`
}

// DiagnoseDeclarations compares declarations with available variable metadata.
func DiagnoseDeclarations(stack *Stack, records map[string]VariableMetadata, locked bool) []DeclarationDiagnostic {
	var out []DeclarationDiagnostic
	if stack == nil {
		return out
	}
	for key, d := range stack.Variables {
		m, ok := records[key]
		count := len(stack.References[key])
		if locked {
			out = append(out, DeclarationDiagnostic{key, "locked_unknown", "store is locked; availability is unknown", count})
		} else if d.IsRequired() && !ok {
			out = append(out, DeclarationDiagnostic{key, "required_unset", "required variable is unset", count})
		}
		if !locked && ok && d.ValueType() != m.Type {
			out = append(out, DeclarationDiagnostic{key, "type_mismatch", "stored type does not match declaration", count})
		}
		if !locked && ok && !d.IsSecret() && m.Secret {
			out = append(out, DeclarationDiagnostic{key, "sensitivity_mismatch", "plaintext declaration does not weaken stored secret", count})
		}
		if !locked && ok && m.Deprecated != "" {
			out = append(out, DeclarationDiagnostic{key, "deprecated", m.Deprecated, count})
		}
		if count == 0 {
			out = append(out, DeclarationDiagnostic{key, "declared_unused", "declared variable has no consumers", count})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// VariableMetadata is the value-free subset of a stored variable used by
// declaration diagnostics.
type VariableMetadata struct {
	Type       string `json:"type"`
	Secret     bool   `json:"secret"`
	Deprecated string `json:"deprecated,omitempty"`
}
