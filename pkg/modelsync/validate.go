package modelsync

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Issue severities.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

// Issue is one validation finding.
type Issue struct {
	Severity string `json:"severity"`
	Field    string `json:"field"`
	Message  string `json:"message"`
}

// HasErrors reports whether any issue is error-severity.
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

var (
	envVarRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	// nameRe covers policy names, provider ids, and router entry names.
	nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	// backendRe additionally allows slashes (LiteLLM model_name values
	// like "openai/qwen3" are legal).
	backendRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:-]*$`)
	// secretPrefixRe matches well-known secret shapes.
	secretPrefixRe = regexp.MustCompile(`^(sk-|sk_|ghp_|gho_|xox[bap]-|AKIA)`)
)

// dangerousTopLevelKeys are LiteLLM config keys that must never travel
// through the policy: an included fragment shallow-overwrites non-list
// keys, so emitting any of these would silently clobber the user's own
// block in the parent config (or vice versa).
var dangerousTopLevelKeys = []string{
	"fallbacks", "general_settings", "litellm_settings",
	"model_list", "router_settings",
}

// dangerousKeyHint explains the include shallow-overwrite hazard once,
// shared by every dangerous-key message.
const dangerousKeyHint = "LiteLLM's include directive extends list keys but silently " +
	"replaces top-level maps, so this setting belongs in your primary LiteLLM " +
	"config.yaml, not in the gridctl-rendered fragment"

// Validate checks the policy and returns findings, most severe first.
// It never touches the network; the only file it may read is the
// declared parent LiteLLM config, to warn about unknown backends.
func (m *Manager) Validate(p *Policy) []Issue {
	var issues []Issue
	add := func(sev, field, msg string) {
		issues = append(issues, Issue{Severity: sev, Field: field, Message: msg})
	}

	if strings.TrimSpace(p.Name) == "" {
		add(SeverityError, "name", "name is required")
	}
	if p.Kind != PolicyKind {
		add(SeverityError, "kind", fmt.Sprintf("kind must be %q, got %q", PolicyKind, p.Kind))
	}
	if p.Router.EntryModel == "" {
		add(SeverityError, "router.entry_model", "entry_model is required (the model name clients select)")
	} else if !nameRe.MatchString(p.Router.EntryModel) {
		add(SeverityError, "router.entry_model", fmt.Sprintf("invalid model name %q", p.Router.EntryModel))
	}

	backends := map[string]bool{}
	if len(p.Backends) == 0 {
		add(SeverityError, "backends", "at least one backend model_name reference is required")
	}
	for _, b := range p.Backends {
		if !backendRe.MatchString(b) {
			add(SeverityError, "backends", fmt.Sprintf("invalid backend model_name %q", b))
			continue
		}
		if backends[b] {
			add(SeverityError, "backends", fmt.Sprintf("duplicate backend %q", b))
		}
		backends[b] = true
	}

	tierSet := map[string]bool{}
	for _, tier := range tierOrder {
		backend := p.Tiers.byName(tier)
		if backend == "" {
			add(SeverityError, "tiers."+tier, "every tier needs a backend assignment")
			continue
		}
		tierSet[tier] = true
		if len(backends) > 0 && !backends[backend] {
			add(SeverityError, "tiers."+tier,
				fmt.Sprintf("references %q, which is not a declared backend", backend))
		}
	}
	if p.Router.DefaultTier == "" {
		add(SeverityError, "router.default_tier", "default_tier is required (where unclassifiable requests land)")
	} else if !tierSet[p.Router.DefaultTier] {
		add(SeverityError, "router.default_tier",
			fmt.Sprintf("%q is not an assigned tier (expected one of %v)", p.Router.DefaultTier, tierOrder))
	}

	if p.Passthrough != nil {
		if _, ok := p.Passthrough["tiers"]; ok {
			add(SeverityError, "passthrough.tiers", "tiers is a typed field; set it at the top level")
		}
		if _, ok := p.Passthrough["dimension_weights"]; ok && len(p.Weights) > 0 {
			add(SeverityWarning, "passthrough.dimension_weights",
				"ignored because weights is set; typed keys win over passthrough")
		}
	}

	if oc := p.Clients.OpenCode; oc != nil {
		validateOpenCodeClient(oc, add)
	}
	if lt := p.Targets.LiteLLM; lt != nil {
		if lt.ConfigPath == "" {
			add(SeverityError, "targets.litellm.config_path",
				"config_path is required so sync knows which LiteLLM config includes the fragment")
		} else if _, err := m.expandPath(lt.ConfigPath); err != nil {
			add(SeverityError, "targets.litellm.config_path", err.Error())
		}
		if lt.FragmentPath != "" {
			if _, err := m.expandPath(lt.FragmentPath); err != nil {
				add(SeverityError, "targets.litellm.fragment_path", err.Error())
			}
		}
	}

	issues = append(issues, lintDangerousKeys(p)...)
	issues = append(issues, lintSecretValues(p)...)
	issues = append(issues, m.lintParentBackends(p, backends)...)

	sort.SliceStable(issues, func(i, j int) bool {
		return issues[i].Severity == SeverityError && issues[j].Severity != SeverityError
	})
	return issues
}

func validateOpenCodeClient(oc *OpenCodeClient, add func(sev, field, msg string)) {
	if oc.ProviderID == "" {
		add(SeverityError, "clients.opencode.provider_id", "provider_id is required")
	} else if !nameRe.MatchString(oc.ProviderID) {
		add(SeverityError, "clients.opencode.provider_id", fmt.Sprintf("invalid provider id %q", oc.ProviderID))
	}
	if oc.BaseURL == "" {
		add(SeverityError, "clients.opencode.base_url", "base_url is required (the LiteLLM /v1 endpoint)")
	} else if u, err := url.Parse(oc.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		add(SeverityError, "clients.opencode.base_url", fmt.Sprintf("%q is not an http(s) URL", oc.BaseURL))
	}
	if oc.APIKeyEnv == "" {
		add(SeverityError, "clients.opencode.api_key_env", "api_key_env is required (an environment variable name, never a literal key)")
	} else if !envVarRe.MatchString(oc.APIKeyEnv) {
		msg := fmt.Sprintf("%q is not an environment variable name (expected e.g. LITELLM_KEY)", oc.APIKeyEnv)
		if looksLikeSecret(oc.APIKeyEnv) {
			msg = "this looks like a literal secret; put the key in an environment variable " +
				"(or the gridctl vault) and reference it by name"
		}
		add(SeverityError, "clients.opencode.api_key_env", msg)
	}
	switch oc.Schema {
	case "", "detect", "v1", "v2":
	default:
		add(SeverityError, "clients.opencode.schema",
			fmt.Sprintf("unknown schema %q (expected v1, v2, or detect)", oc.Schema))
	}
}

// lintDangerousKeys rejects unknown top-level keys that would collide
// with LiteLLM's own config surface.
func lintDangerousKeys(p *Policy) []Issue {
	var issues []Issue
	for _, key := range dangerousTopLevelKeys {
		if _, ok := p.Extra[key]; ok {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Field:    key,
				Message:  dangerousKeyHint,
			})
		}
	}
	return issues
}

// lintSecretValues walks string values in secret-shaped positions and
// hard-fails on anything that looks like a literal credential.
func lintSecretValues(p *Policy) []Issue {
	var issues []Issue
	check := func(field, value string) {
		if looksLikeSecret(value) {
			issues = append(issues, Issue{
				Severity: SeverityError,
				Field:    field,
				Message: "this looks like a literal secret; rendered output carries only " +
					"os.environ/ and {env:...} references, so store the key in an " +
					"environment variable (or the gridctl vault) and reference it by name",
			})
		}
	}
	walkSecretKeys("passthrough", p.Passthrough, check)
	walkSecretKeys("", p.Extra, check)
	return issues
}

// secretKeyRe marks map keys whose values must never be literals.
var secretKeyRe = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password)`)

func walkSecretKeys(prefix string, m map[string]any, check func(field, value string)) {
	for k, v := range m {
		field := k
		if prefix != "" {
			field = prefix + "." + k
		}
		switch val := v.(type) {
		case string:
			if secretKeyRe.MatchString(k) && !strings.HasPrefix(val, "os.environ/") {
				check(field, val)
			}
		case map[string]any:
			walkSecretKeys(field, val, check)
		}
	}
}

// looksLikeSecret is a heuristic over known prefixes and high-entropy
// mixed-class strings.
func looksLikeSecret(s string) bool {
	if secretPrefixRe.MatchString(s) {
		return true
	}
	if len(s) < 20 || strings.ContainsAny(s, " \t") {
		return false
	}
	var upper, lower, digit bool
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		}
	}
	return upper && lower && digit
}

// lintParentBackends warns about tier backends missing from the parent
// LiteLLM config's model_list, when the parent is declared and
// readable. Best-effort: an unreadable parent is its own sync-time
// error, not a validation failure.
func (m *Manager) lintParentBackends(p *Policy, backends map[string]bool) []Issue {
	if p.Targets.LiteLLM == nil || p.Targets.LiteLLM.ConfigPath == "" {
		return nil
	}
	parent, err := m.expandPath(p.Targets.LiteLLM.ConfigPath)
	if err != nil {
		return nil
	}
	scan, err := ParseLiteLLMConfig(parent)
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, name := range scan.ModelNames {
		known[name] = true
	}
	var missing []string
	for b := range backends {
		if !known[b] {
			missing = append(missing, b)
		}
	}
	sort.Strings(missing)
	var issues []Issue
	for _, b := range missing {
		issues = append(issues, Issue{
			Severity: SeverityWarning,
			Field:    "backends",
			Message: fmt.Sprintf("%q is not in the model_list of %s; LiteLLM will not "+
				"know this model unless you add it there", b, parent),
		})
	}
	return issues
}
