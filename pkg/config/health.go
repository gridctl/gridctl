package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/flags"
	"github.com/gridctl/gridctl/pkg/registry"
)

// IssueSeverity represents the severity level of a validation issue.
type IssueSeverity string

const (
	SeverityError   IssueSeverity = "error"
	SeverityWarning IssueSeverity = "warning"
	SeverityInfo    IssueSeverity = "info"
)

// ValidationIssue is a validation finding with severity level.
type ValidationIssue struct {
	Field    string        `json:"field"`
	Message  string        `json:"message"`
	Severity IssueSeverity `json:"severity"`
	// Line is the 1-based source line for strict-decode findings. Omitted
	// when the checker cannot name a line.
	Line int `json:"line,omitempty"`
}

// ValidationResult holds the complete output of spec validation.
type ValidationResult struct {
	Execution    map[string]*execution.Report `json:"execution,omitempty"`
	Valid        bool                         `json:"valid"`
	ErrorCount   int                          `json:"errorCount"`
	WarningCount int                          `json:"warningCount"`
	Issues       []ValidationIssue            `json:"issues"`
	// Override is the override file merged or discovered during validation.
	Override string `json:"override,omitempty"`
}

// SpecHealth aggregates validation, drift, and dependency status.
type SpecHealth struct {
	Validation   ValidationStatus `json:"validation"`
	Drift        DriftStatus      `json:"drift"`
	Dependencies DependencyStatus `json:"dependencies"`

	// Replicas reports live per-replica health for every server that has
	// more than one replica registered. Servers with replicas <= 1 are
	// omitted so the shape is backward compatible with single-replica
	// deployments. Keyed by server name.
	Replicas map[string][]ReplicaHealth `json:"replicas,omitempty"`

	// Override is the override file active for this health check.
	Override string `json:"override,omitempty"`
}

// ReplicaHealth describes the live state of one replica in a server's
// ReplicaSet. Durations use seconds so the JSON representation does not
// depend on Go's time formatting.
type ReplicaHealth struct {
	Execution         *execution.Report `json:"execution,omitempty"`
	ReplicaID         int               `json:"replicaId"`
	State             string            `json:"state"` // "healthy" | "unhealthy" | "restarting"
	InFlight          int64             `json:"inFlight"`
	UptimeSeconds     int64             `json:"uptimeSeconds,omitempty"`
	LastError         string            `json:"lastError,omitempty"`
	NextRetrySeconds  int64             `json:"nextRetrySeconds,omitempty"`
	RestartAttempts   uint32            `json:"restartAttempts,omitempty"`
	PID               int               `json:"pid,omitempty"`
	ContainerID       string            `json:"containerId,omitempty"`
	RestartPolicy     string            `json:"restartPolicy,omitempty"`
	RestartExhausted  bool              `json:"restartExhausted,omitempty"`
	ContainerRestarts uint32            `json:"containerRestarts,omitempty"`
}

// ValidationStatus summarizes the spec validation state.
type ValidationStatus struct {
	Status       string `json:"status"` // "valid", "warnings", "errors"
	ErrorCount   int    `json:"errorCount"`
	WarningCount int    `json:"warningCount"`
}

// DriftStatus summarizes drift between spec and running state.
type DriftStatus struct {
	Status  string   `json:"status"` // "in-sync", "drifted", "unknown"
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

// DependencyStatus summarizes skill dependency resolution.
type DependencyStatus struct {
	Status  string   `json:"status"` // "resolved", "missing"
	Missing []string `json:"missing,omitempty"`
}

// ValidateWithIssues runs full validation and returns structured issues with severity.
// This wraps the existing Validate() and adds warning-level checks.
func ValidateWithIssues(s *Stack) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Run existing validation to get errors
	if err := Validate(s); err != nil {
		result.Valid = false
		if ve, ok := err.(ValidationErrors); ok {
			for _, e := range ve {
				result.Issues = append(result.Issues, e.ToIssue(SeverityError))
				result.ErrorCount++
			}
		} else {
			// Unexpected error type from validation
			result.Issues = append(result.Issues, ValidationIssue{
				Field:    "stack",
				Message:  err.Error(),
				Severity: SeverityError,
			})
			result.ErrorCount++
		}
	}

	// Add warning-level checks. Disabled servers stay in scope: a parked
	// server is still a spec, and warnings about it remain useful.
	result.addWarnings(s)
	for _, server := range s.MCPServers {
		if server.Execution == nil {
			continue
		}
		contract, err := ResolveExecution(server)
		if err != nil {
			continue
		} // The field error is already included above.
		if result.Execution == nil {
			result.Execution = map[string]*execution.Report{}
		}
		result.Execution[server.Name] = execution.RequestedReport(contract)
	}

	return result
}

// addWarnings appends warning-level issues for non-critical findings.
func (r *ValidationResult) addWarnings(s *Stack) {
	hasNetworks := len(s.Networks) > 0

	// Warn about network field set on non-container servers in simple mode
	if !hasNetworks {
		for i, srv := range s.MCPServers {
			if srv.Network != "" && !srv.IsContainerBased() {
				r.Issues = append(r.Issues, ValidationIssue{
					Field:    fmt.Sprintf("mcp-servers[%d].network", i),
					Message:  "network field ignored for non-container servers",
					Severity: SeverityWarning,
				})
				r.WarningCount++
			}
		}
	}

	// Warn about TLS cert/key/ca files that don't exist yet (may be created before apply)
	for i, srv := range s.MCPServers {
		if srv.OpenAPI == nil || srv.OpenAPI.TLS == nil {
			continue
		}
		tlsPrefix := fmt.Sprintf("mcp-servers[%d].openapi.tls", i)
		tls := srv.OpenAPI.TLS
		for _, f := range []struct{ field, path string }{
			{tlsPrefix + ".certFile", tls.CertFile},
			{tlsPrefix + ".keyFile", tls.KeyFile},
			{tlsPrefix + ".caFile", tls.CaFile},
		} {
			if f.path == "" {
				continue
			}
			if _, err := os.Stat(f.path); err != nil {
				r.Issues = append(r.Issues, ValidationIssue{
					Field:    f.field,
					Message:  fmt.Sprintf("file not found or not readable: %s", f.path),
					Severity: SeverityWarning,
				})
				r.WarningCount++
			}
		}
	}

	// Warn about gateway without auth
	if s.Gateway != nil && s.Gateway.Auth == nil {
		r.Issues = append(r.Issues, ValidationIssue{
			Field:    "gateway.auth",
			Message:  "no authentication configured — gateway is publicly accessible",
			Severity: SeverityWarning,
		})
		r.WarningCount++
	}

	r.addHiddenToolWarnings(s)
	r.addExperimentalIssues(flags.Default(), s)
	r.addSkillsPolicyIssues(s)
	r.addPolicyApprovalWarnings(s)
	r.addModelPreferencesIssues(s)
}

// addPolicyApprovalWarnings warns for every require_approval rule. The
// action is parsed and reported as authored, and enforced as block until
// call approvals exist. Warnings do not change validate's exit codes
// beyond the documented 2 for warnings-only.
func (r *ValidationResult) addPolicyApprovalWarnings(s *Stack) {
	if s == nil || s.Policy == nil {
		return
	}
	for i, rule := range s.Policy.Rules {
		if rule.Action != "require_approval" {
			continue
		}
		r.Issues = append(r.Issues, ValidationIssue{
			Field:    fmt.Sprintf("policy.rules[%d].action", i),
			Message:  "require_approval is enforced as block until call approvals are supported",
			Severity: SeverityWarning,
		})
		r.WarningCount++
	}
}

// addHiddenToolWarnings warns when a profile or a group exclude names a tool
// that a non-empty server whitelist hides. Those references cannot succeed,
// but unlike a group tools or overrides entry they do not make a call fail
// that would otherwise have been allowed.
func (r *ValidationResult) addHiddenToolWarnings(s *Stack) {
	whitelists := serverWhitelists(s)
	if len(whitelists) == 0 {
		return
	}
	if s.Clients != nil {
		for id, profile := range s.Clients.Profiles {
			for i, tool := range profile.Tools {
				server, _, ok := splitPrefixedToolName(tool)
				if !ok {
					continue
				}
				msg, hidden := hiddenToolMessage(whitelists, server, tool)
				if !hidden {
					continue
				}
				r.Issues = append(r.Issues, ValidationIssue{
					Field:    fmt.Sprintf("clients.profiles[%s].tools[%d]", id, i),
					Message:  msg + "; the profile may be wider than the whitelist on purpose",
					Severity: SeverityWarning,
				})
				r.WarningCount++
			}
		}
	}
	for name, group := range s.Groups {
		for i, tool := range group.Exclude {
			server, _, ok := splitPrefixedToolName(tool)
			if !ok {
				continue
			}
			msg, hidden := hiddenToolMessage(whitelists, server, tool)
			if !hidden {
				continue
			}
			r.Issues = append(r.Issues, ValidationIssue{
				Field:    fmt.Sprintf("groups[%s].exclude[%d]", name, i),
				Message:  msg + "; excluding a hidden tool has no effect",
				Severity: SeverityWarning,
			})
			r.WarningCount++
		}
	}
}

// addModelPreferencesIssues reports on the `model_preferences:` block.
// All findings are advisory (warnings and info; the block can never
// fail validation): model alias vocabularies churn on client-release
// timescales, and an unknown override name may simply arrive later via
// pack. The value check is `model-preference-unknown-alias`; content
// checks that need the registry (unhonored targets, portability) join
// at the cmd layer like the skills-policy apply warnings.
func (r *ValidationResult) addModelPreferencesIssues(s *Stack) {
	if s.ModelPreferences == nil {
		return
	}
	checkScope := func(name string, scope *ModelPreferenceScope) {
		if scope == nil {
			return
		}
		warnValue := func(field, value string) {
			if value == "" || registry.IsKnownModelValue(value) {
				return
			}
			r.Issues = append(r.Issues, ValidationIssue{
				Field:    field,
				Message:  fmt.Sprintf("model-preference-unknown-alias: %q is neither a documented alias (%s) nor shaped like a full model ID; clients fall back to their own default for values they cannot resolve", value, strings.Join(registry.KnownModelAliases(), ", ")),
				Severity: SeverityWarning,
			})
			r.WarningCount++
		}
		warnValue("model_preferences."+name+".default", scope.Default)
		keys := make([]string, 0, len(scope.Overrides))
		for k := range scope.Overrides {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnValue("model_preferences."+name+".overrides."+k, scope.Overrides[k])
		}
		if scope.Rewrite {
			detail := fmt.Sprintf("%d override(s)", len(scope.Overrides))
			if scope.Default != "" {
				detail += fmt.Sprintf(", default %q applies to every projected %s with no declared preference (affected projections are forced to copy channel)", scope.Default, strings.TrimSuffix(name, "s"))
			}
			r.Issues = append(r.Issues, ValidationIssue{
				Field:    "model_preferences." + name,
				Message:  "projection rewrite enabled for " + name + ": " + detail,
				Severity: SeverityInfo,
			})
		}
	}
	checkScope("skills", s.ModelPreferences.Skills)
	checkScope("agents", s.ModelPreferences.Agents)
}

// addSkillsPolicyIssues reports on the `skills:` exposure block. Validate has
// no registry access, so which skills a rule actually hides is an apply-time
// warning; here the checks are shape-level: a default-deny block with no
// allow list hides every skill (legal, but worth a deliberate look), and a
// present block surfaces as an info line so `gridctl validate` shows that a
// skill exposure policy is in force.
func (r *ValidationResult) addSkillsPolicyIssues(s *Stack) {
	if s.Skills == nil {
		return
	}
	if s.Skills.Default == "deny" && len(s.Skills.Allow) == 0 {
		r.Issues = append(r.Issues, ValidationIssue{
			Field:    "skills.default",
			Message:  "default: deny with no allow list hides every registry skill from clients and projection",
			Severity: SeverityWarning,
		})
		r.WarningCount++
	}
	r.Issues = append(r.Issues, ValidationIssue{
		Field:    "skills",
		Message:  fmt.Sprintf("skill exposure policy in force (%d allow, %d deny pattern(s)); denied active skills are named at apply time", len(s.Skills.Allow), len(s.Skills.Deny)),
		Severity: SeverityInfo,
	})
}

// addExperimentalIssues reports on the `experimental:` map: unknown flag
// names and concluded (graduated/removed) names surface as warnings with the
// registry's migration text, and each enabled valid flag surfaces as an
// info-level line so `gridctl validate` and the UI panel show active
// experiments. Warnings only — an unrecognized name never blocks a deploy
// (Article IX: a stack written against a newer gridctl must still start).
func (r *ValidationResult) addExperimentalIssues(reg *flags.Registry, s *Stack) {
	for _, w := range flags.CheckNames(reg, s.Experimental) {
		r.Issues = append(r.Issues, ValidationIssue{
			Field:    "experimental." + w.Name,
			Message:  w.Message,
			Severity: SeverityWarning,
		})
		r.WarningCount++
	}
	names := make([]string, 0, len(s.Experimental))
	for name, on := range s.Experimental {
		if !on {
			continue
		}
		if f, ok := reg.Lookup(name); ok && f.Stage == flags.StageExperimental {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		r.Issues = append(r.Issues, ValidationIssue{
			Field:    "experimental." + name,
			Message:  "experimental flag enabled",
			Severity: SeverityInfo,
		})
	}
}

// ExpandStackVarsWithEnv expands environment variable references in stack fields.
func ExpandStackVarsWithEnv(s *Stack) {
	_ = ExpandStackVarsWithEnvChecked(s)
}

// ExpandStackVarsWithEnvChecked expands environment references and returns a
// typed error when a reserved internal credential is referenced.
func ExpandStackVarsWithEnvChecked(s *Stack) error {
	_, _, problems := expandStackVarsResolved(s, newReferenceResolver(nil))
	if len(problems) > 0 {
		return problems[0]
	}
	return nil
}

// ValidateStackFile loads a stack file and validates it without deploying.
// Returns the parsed stack (for further use) and the validation result.
// Optional LoadOption values select or suppress an override file. The
// override is merged after decode and before environment expansion. This
// parser does not resolve extends, so an override that names a server only
// a parent declares is an error when the file has no extends and a warning
// when it does.
func ValidateStackFile(path string, opts ...LoadOption) (*Stack, *ValidationResult, error) {
	var cfg loadConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("reading stack file: %w", err)
	}

	stack, err := DecodeStack(data)
	if err != nil {
		return nil, nil, err
	}

	if err := mergeValidateOverride(stack, path, &cfg); err != nil {
		return nil, nil, err
	}

	// Expand environment variables without allowing bootstrap credentials to
	// cross into the parsed stack used by validation and export callers.
	_, _, resolutionProblems := expandStackVarsResolved(stack, newReferenceResolver(nil))
	if len(resolutionProblems) > 0 {
		return nil, nil, fmt.Errorf("resolving stack variable: %w", resolutionProblems[0])
	}

	// Apply defaults
	stack.SetDefaults()

	// Validate with severity. Disabled servers stay in scope.
	result := ValidateWithIssues(stack)
	for _, warning := range stack.overrideWarnings {
		result.Issues = append(result.Issues, ValidationIssue{
			Field:    "override",
			Message:  warning,
			Severity: SeverityWarning,
		})
		result.WarningCount++
	}
	if stack.OverridePath != "" {
		result.Override = stack.OverridePath
	}

	return stack, result, nil
}

// mergeValidateOverride applies the same selection LoadStack uses, but does
// not resolve extends. Unknown servers are errors unless the file declares
// extends, in which case they are warnings: the parent may declare them.
func mergeValidateOverride(stack *Stack, path string, cfg *loadConfig) error {
	if cfg.noOverride && cfg.overridePath != "" {
		return fmt.Errorf("override: --override and --no-override are mutually exclusive")
	}
	if cfg.noOverride {
		return nil
	}
	overridePath, found, err := DiscoverOverride(path, OverrideSelection{
		Path:     cfg.overridePath,
		Required: cfg.overrideRequired,
	})
	if err != nil || !found {
		return err
	}
	warnUnknown := stack.Extends != ""
	return mergeOverrideFile(stack, overridePath, warnUnknown)
}
