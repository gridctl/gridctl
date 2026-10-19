package config

import (
	"os"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/vault"
)

// Resolver looks up a variable by name. Returns value and whether it exists.
type Resolver func(name string) (string, bool)

// ResolutionVerdict identifies where a variable lookup resolved.
type ResolutionVerdict string

const (
	ResolutionStore       ResolutionVerdict = "store"
	ResolutionEnvFallback ResolutionVerdict = "env_fallback"
	ResolutionUnset       ResolutionVerdict = "unset"
	ResolutionDenied      ResolutionVerdict = "denied"
)

// ResolutionResult carries a value only when resolution succeeded. Denied
// results include a typed error suitable for errors.As.
type ResolutionResult struct {
	Value   string
	Verdict ResolutionVerdict
	Error   error
}

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
		if value, ok := vault.Get(name); ok {
			return value, true
		}
		return os.LookupEnv(name)
	}
}

// ResolveVariable applies store-first, environment-second precedence while
// denying internal credential keys without environment fallback.
func ResolveVariable(store VaultLookup, name string) ResolutionResult {
	if vault.IsInternalCredential(name) {
		return ResolutionResult{Verdict: ResolutionDenied, Error: vault.NewInternalCredentialError(name)}
	}
	if store != nil {
		if value, ok := store.Get(name); ok {
			return ResolutionResult{Value: value, Verdict: ResolutionStore}
		}
	}
	if value, ok := os.LookupEnv(name); ok {
		return ResolutionResult{Value: value, Verdict: ResolutionEnvFallback}
	}
	return ResolutionResult{Verdict: ResolutionUnset}
}

type referenceResolver func(name string, storeReference bool) ResolutionResult

func newReferenceResolver(store VaultLookup) referenceResolver {
	return func(name string, storeReference bool) ResolutionResult {
		if storeReference {
			return ResolveVariable(store, name)
		}
		if exactInternalCredential(name) {
			return ResolutionResult{Verdict: ResolutionDenied, Error: vault.NewInternalCredentialError(name)}
		}
		if store != nil {
			if value, ok := store.Get(name); ok {
				return ResolutionResult{Value: value, Verdict: ResolutionStore}
			}
		}
		if value, ok := os.LookupEnv(name); ok {
			return ResolutionResult{Value: value, Verdict: ResolutionEnvFallback}
		}
		return ResolutionResult{Verdict: ResolutionUnset}
	}
}

func exactInternalCredential(name string) bool {
	for _, key := range vault.InternalCredentialKeys() {
		if key == name {
			return true
		}
	}
	return false
}

// expandRegex matches all variable reference forms in a single pass:
//   - $VAR                — simple variable (backward compat with os.ExpandEnv)
//   - ${VAR}              — braced variable reference
//   - ${VAR:-default}     — use default if undefined or empty
//   - ${VAR:+replacement} — use replacement if defined and non-empty
//   - ${var:KEY}          — variable store reference (canonical)
//   - ${vault:KEY}        — alias for ${var:KEY}, retained for back-compat
//   - ${secret:prov/path} — external secret reference, left literal at load
//
// The braced form cannot consume ${secret:prov/path} (its operator class is
// [+-]), so the secret alternation is appended rather than ordered ahead of
// it. Consumers look groups up by name. Positional indexes shift if a group
// is added, and export credential checks used to read them by position.
var expandRegex = regexp.MustCompile(
	`\$\{(?:(?P<prefix>vault|var):)?(?P<name>[a-zA-Z_][a-zA-Z0-9_]*)(?::(?P<op>[+-])(?P<operand>[^}]*))?\}` +
		`|` +
		`\$(?P<bare>[a-zA-Z_][a-zA-Z0-9_]*)` +
		`|` +
		`\$\{secret:(?P<sprov>[a-z][a-z0-9-]*)/(?P<spath>[^}]+)\}`,
)

// ContainsExpansion reports whether s includes a recognized $VAR or ${...} form,
// including default and replacement operands.
func ContainsExpansion(s string) bool {
	return expandRegex.MatchString(s)
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

// ExpandStringResolved expands with store-aware resolution and returns typed
// problems for denied internal credentials. It preserves ordinary environment
// interpolation while applying the reserved namespace to store references.
func ExpandStringResolved(s string, store VaultLookup) (expanded string, unresolvedVault []string, emptyEnvVars []string, problems []error) {
	result, problems := expandStringRefs(s, newReferenceResolver(store))
	return result.expanded, result.unresolvedVault, result.emptyEnvVars, problems
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
	result, denied := expandStringRefs(s, func(name string, _ bool) ResolutionResult {
		value, ok := resolve(name)
		if !ok {
			return ResolutionResult{Verdict: ResolutionUnset}
		}
		return ResolutionResult{Value: value, Verdict: ResolutionEnvFallback}
	})
	_ = denied
	return result.expanded, result.storeRefs, result.unresolvedVault, result.emptyEnvVars
}

type expansionResult struct {
	expanded        string
	storeRefs       []string
	secretRefs      []string
	unresolvedVault []string
	emptyEnvVars    []string
	emptySecretPath bool
}

func expandGroup(parts []string, name string) string {
	idx := expandRegex.SubexpIndex(name)
	if idx < 0 || idx >= len(parts) {
		return ""
	}
	return parts[idx]
}

// SecretRefKey returns the index key for a provider and path.
func SecretRefKey(provider, path string) string {
	return "secret:" + provider + "/" + path
}

// ParseSecretRefKey splits a secret:<provider>/<path> key. The path may
// contain slashes.
func ParseSecretRefKey(key string) (provider, path string, ok bool) {
	rest, found := strings.CutPrefix(key, "secret:")
	if !found {
		return "", "", false
	}
	provider, path, found = strings.Cut(rest, "/")
	if !found || provider == "" || strings.TrimSpace(path) == "" {
		return "", "", false
	}
	return provider, path, true
}

func containsSecretRef(s string) bool {
	if !strings.Contains(s, "${secret:") {
		return false
	}
	matches := expandRegex.FindAllStringSubmatch(s, -1)
	prov := expandRegex.SubexpIndex("sprov")
	for _, match := range matches {
		if prov >= 0 && prov < len(match) && match[prov] != "" {
			return true
		}
	}
	return false
}

func replaceSecretRefs(s string, values map[string]string) string {
	if !strings.Contains(s, "${secret:") || len(values) == 0 {
		return s
	}
	return expandRegex.ReplaceAllStringFunc(s, func(match string) string {
		parts := expandRegex.FindStringSubmatch(match)
		provider := expandGroup(parts, "sprov")
		if provider == "" {
			return match
		}
		path := strings.TrimSpace(expandGroup(parts, "spath"))
		if path == "" {
			return match
		}
		if value, ok := values[SecretRefKey(provider, path)]; ok {
			return value
		}
		return match
	})
}

func expandStringRefs(s string, resolve referenceResolver) (expansionResult, []error) {
	var result expansionResult
	var denied []error

	result.expanded = expandRegex.ReplaceAllStringFunc(s, func(match string) string {
		parts := expandRegex.FindStringSubmatch(match)
		if len(parts) == 0 {
			return match
		}

		// Secret references stay literal. Resolution is a later pass.
		if provider := expandGroup(parts, "sprov"); provider != "" {
			path := strings.TrimSpace(expandGroup(parts, "spath"))
			if path == "" {
				result.emptySecretPath = true
				return match
			}
			result.secretRefs = append(result.secretRefs, SecretRefKey(provider, path))
			return match
		}

		// Check if this is a bare $VAR match.
		if varName := expandGroup(parts, "bare"); varName != "" {
			resolution := resolve(varName, false)
			if resolution.Error != nil {
				denied = append(denied, resolution.Error)
				return match
			}
			value := resolution.Value
			exists := resolution.Verdict != ResolutionUnset
			if !exists || value == "" {
				result.emptyEnvVars = append(result.emptyEnvVars, varName)
			}
			return value
		}

		// Braced ${...} form. `var` and `vault` are both store-prefixed
		// references; `var` is canonical and `vault` is a back-compat alias
		// that resolves identically.
		prefix := expandGroup(parts, "prefix")
		isStoreRef := prefix == "vault" || prefix == "var"
		varName := expandGroup(parts, "name")
		op := expandGroup(parts, "op")
		operand := expandGroup(parts, "operand")

		// Record the reference up front, independent of resolution outcome,
		// so usage tracing sees every ${var:KEY} site even when the key is
		// unresolved or supplied via the environment.
		if isStoreRef {
			result.storeRefs = append(result.storeRefs, varName)
		}

		resolution := resolve(varName, isStoreRef)
		if resolution.Error != nil {
			denied = append(denied, resolution.Error)
			return match
		}
		value := resolution.Value
		exists := resolution.Verdict != ResolutionUnset

		// No operator
		if op == "" {
			if isStoreRef && !exists {
				result.unresolvedVault = append(result.unresolvedVault, varName)
				return match // leave as-is for error reporting
			}
			if !isStoreRef && !exists {
				result.emptyEnvVars = append(result.emptyEnvVars, varName)
			} else if !isStoreRef && value == "" && exists {
				result.emptyEnvVars = append(result.emptyEnvVars, varName)
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

	return result, denied
}
