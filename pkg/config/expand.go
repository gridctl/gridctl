package config

import (
	"log/slog"
	"os"
	"regexp"
	"sync"
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
//   - ${var:KEY}          — variable store reference (canonical)
//   - ${vault:KEY}        — deprecated alias for ${var:KEY}; warned once per process
//
// The alternation tries the braced form first (longer match), then the bare $VAR form.
var expandRegex = regexp.MustCompile(
	`\$\{(?:(vault|var):)?([a-zA-Z_][a-zA-Z0-9_]*)(?::([+-])([^}]*))?\}` + // ${...} forms
		`|` +
		`\$([a-zA-Z_][a-zA-Z0-9_]*)`, // $VAR form
)

// vaultSyntaxDeprecationOnce ensures the "${vault:KEY}" syntax deprecation
// banner prints at most once per process, even if a stack has dozens of
// references. Resetting is exposed for tests; production code should never
// touch it.
var vaultSyntaxDeprecationOnce sync.Once

// warnVaultSyntaxDeprecated logs the once-per-process deprecation warning for
// stack files that still use ${vault:KEY}. The canonical syntax going forward
// is ${var:KEY}; both resolve identically through the beta cycle.
func warnVaultSyntaxDeprecated() {
	vaultSyntaxDeprecationOnce.Do(func() {
		slog.Warn(`"${vault:KEY}" syntax is deprecated, use "${var:KEY}" instead. Removal at v1.0.`)
	})
}

// ExpandString expands variable references in a string using the given resolver.
// All patterns are matched in a single pass to prevent double-expansion of values
// that contain dollar signs.
//
// Returns the expanded string, any unresolved vault references, and env vars
// that resolved to empty.
func ExpandString(s string, resolve Resolver) (expanded string, unresolvedVault []string, emptyEnvVars []string) {
	expanded, _, unresolvedVault, emptyEnvVars = ExpandStringRefs(s, resolve)
	return expanded, unresolvedVault, emptyEnvVars
}

// ExpandStringRefs is ExpandString plus storeRefs: the variable-store keys
// (the KEY in ${var:KEY} / ${vault:KEY}) referenced by s, in first-seen order.
//
// storeRefs is captured from the parsed grammar *before* resolution, so it
// records what s references regardless of whether each key resolves — this is
// the basis for usage tracing and is why the index can never drift from what
// expansion actually recognizes. Bare $VAR / ${VAR} env-style references are
// NOT store references and are deliberately excluded.
func ExpandStringRefs(s string, resolve Resolver) (expanded string, storeRefs []string, unresolvedVault []string, emptyEnvVars []string) {
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

		// Braced ${...} form. `var` and `vault` are both store-prefixed
		// references; `var` is canonical and `vault` is the deprecated alias
		// retained through beta.
		prefix := parts[1]
		isStoreRef := prefix == "vault" || prefix == "var"
		if prefix == "vault" {
			warnVaultSyntaxDeprecated()
		}
		varName := parts[2]
		op := parts[3]
		operand := parts[4]

		// Record the reference up front, independent of resolution outcome,
		// so usage tracing sees every ${var:KEY} site even when the key is
		// unresolved or supplied via the environment.
		if isStoreRef {
			storeRefs = append(storeRefs, varName)
		}

		value, exists := resolve(varName)

		// No operator
		if op == "" {
			if isStoreRef && !exists {
				unresolvedVault = append(unresolvedVault, varName)
				return match // leave as-is for error reporting
			}
			if !isStoreRef && !exists {
				emptyEnvVars = append(emptyEnvVars, varName)
			} else if !isStoreRef && value == "" && exists {
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

	return expanded, storeRefs, unresolvedVault, emptyEnvVars
}
