package config

import (
	"os"
	"regexp"
)

// Resolver looks up a variable by name. Returns value and whether it exists.
type Resolver func(name string) (string, bool)

// VaultLookup is the interface the vault store must satisfy.
type VaultLookup interface {
	Get(key string) (string, bool)
}

// VaultSetLookup extends VaultLookup with set operations for secrets.sets support.
type VaultSetLookup interface {
	VaultLookup
	GetSetSecrets(setName string) []VaultSecret
}

// VaultSecret is a minimal secret view for set lookups.
type VaultSecret struct {
	Key   string
	Value string
}

// EnvResolver returns a resolver that checks os.LookupEnv.
func EnvResolver() Resolver {
	return os.LookupEnv
}

// VaultResolver returns a resolver that checks vault first, then env.
func VaultResolver(vault VaultLookup) Resolver {
	return func(name string) (string, bool) {
		if v, ok := vault.Get(name); ok {
			return v, true
		}
		return os.LookupEnv(name)
	}
}

// expandRegex matches all variable reference forms in a single pass:
//   - $VAR                — simple variable (backward compat with os.ExpandEnv)
//   - ${VAR}              — braced variable reference
//   - ${VAR:-default}     — use default if undefined or empty
//   - ${VAR:+replacement} — use replacement if defined and non-empty
//   - ${vault:KEY}        — vault reference
//
// The alternation tries the braced form first (longer match), then the bare $VAR form.
var expandRegex = regexp.MustCompile(
	`\$\{(?:(vault):)?([a-zA-Z_][a-zA-Z0-9_]*)(?::([+-])([^}]*))?\}` + // ${...} forms
		`|` +
		`\$([a-zA-Z_][a-zA-Z0-9_]*)`, // $VAR form
)

// ExpandString expands variable references in a string using the given resolver.
// All patterns are matched in a single pass to prevent double-expansion of values
// that contain dollar signs.
//
// Returns the expanded string, any unresolved vault references, and env vars
// that resolved to empty.
func ExpandString(s string, resolve Resolver) (expanded string, unresolvedVault []string, emptyEnvVars []string) {
	if resolve == nil {
		resolve = EnvResolver()
	}

	expanded = expandRegex.ReplaceAllStringFunc(s, func(match string) string {
		parts := expandRegex.FindStringSubmatch(match)
		if len(parts) < 6 {
			return match
		}

		// Check if this is a bare $VAR match (group 5)
		if parts[5] != "" {
			varName := parts[5]
			value, exists := resolve(varName)
			if !exists || value == "" {
				emptyEnvVars = append(emptyEnvVars, varName)
			}
			return value
		}

		// Braced ${...} form
		isVault := parts[1] == "vault"
		varName := parts[2]
		op := parts[3]
		operand := parts[4]

		value, exists := resolve(varName)

		// No operator
		if op == "" {
			if isVault && !exists {
				unresolvedVault = append(unresolvedVault, varName)
				return match // leave as-is for error reporting
			}
			if !isVault && !exists {
				emptyEnvVars = append(emptyEnvVars, varName)
			} else if !isVault && value == "" && exists {
				emptyEnvVars = append(emptyEnvVars, varName)
			}
			return value
		}

		switch op {
		case "-":
			// ${VAR:-default} — use default if undefined or empty
			if value == "" {
				return operand
			}
			return value
		case "+":
			// ${VAR:+replacement} — use replacement if defined and non-empty
			if exists && value != "" {
				return operand
			}
			return ""
		default:
			return match
		}
	})

	return expanded, unresolvedVault, emptyEnvVars
}
