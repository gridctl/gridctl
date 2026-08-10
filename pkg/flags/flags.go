// Package flags is gridctl's experimental feature-flag registry. A flag is
// born experimental and off by default, is enabled per stack through the
// top-level `experimental:` map in stack.yaml (or per process through a
// GRIDCTL_EXPERIMENTAL_<NAME> environment variable), and graduates by having
// its behavior promoted to a real config block. Registry entries are never
// deleted: a graduated or removed flag keeps its entry so a stale stack.yaml
// gets a specific migration message instead of a generic unknown-key warning.
//
// Stage semantics follow the OpenTelemetry Collector featuregate model.
// Setting an unknown or concluded flag name is always a warning, never an
// error: a stack.yaml written against a newer gridctl must still start on an
// older one (Article IX).
package flags

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"

	"github.com/gridctl/gridctl/pkg/env"
)

// Stage is a flag's lifecycle position.
type Stage string

const (
	// StageExperimental flags are off by default and settable via the
	// `experimental:` map or env override.
	StageExperimental Stage = "experimental"
	// StageGraduated flags have been promoted to a real config block.
	// Setting one warns with its migration message; the value is ignored.
	StageGraduated Stage = "graduated"
	// StageRemoved flags no longer exist in any form. Setting one warns
	// with its migration message; the value is ignored.
	StageRemoved Stage = "removed"
)

// Flag is one registry entry.
type Flag struct {
	// Name is the snake_case flag key, spelled identically to the stable
	// config key the feature will graduate to.
	Name string
	// Description is the one-line summary shown in docs and the UI.
	Description string
	// Stage is the lifecycle position.
	Stage Stage
	// Since is the release that introduced the flag (bare semver, e.g. "0.1.0").
	Since string
	// GraduatesBy is the release by which an experimental flag must
	// graduate or have its deadline deliberately extended. Required for
	// experimental flags; the enforcement test fails past it.
	GraduatesBy string
	// Message is the migration text for graduated and removed flags, e.g.
	// "graduated in 0.2.0; remove the entry, the feature is now always on".
	Message string
}

// EnvVar returns the environment variable that overrides this flag.
func (f Flag) EnvVar() string {
	return "GRIDCTL_EXPERIMENTAL_" + strings.ToUpper(f.Name)
}

// Registry is an ordered, indexed set of flags.
type Registry struct {
	ordered []Flag
	index   map[string]Flag
}

// NewRegistry validates and indexes a set of flags. It returns an error for
// duplicate or empty names, a missing Since, an experimental flag without a
// GraduatesBy, or a concluded flag without a migration Message.
func NewRegistry(entries ...Flag) (*Registry, error) {
	r := &Registry{index: make(map[string]Flag, len(entries))}
	for _, f := range entries {
		if f.Name == "" {
			return nil, fmt.Errorf("flag with empty name")
		}
		if strings.ToLower(f.Name) != f.Name || strings.ContainsAny(f.Name, " -") {
			return nil, fmt.Errorf("flag %q: name must be snake_case", f.Name)
		}
		if _, dup := r.index[f.Name]; dup {
			return nil, fmt.Errorf("flag %q registered twice", f.Name)
		}
		if f.Since == "" {
			return nil, fmt.Errorf("flag %q: Since is required", f.Name)
		}
		switch f.Stage {
		case StageExperimental:
			if f.GraduatesBy == "" {
				return nil, fmt.Errorf("flag %q: GraduatesBy is required for experimental flags", f.Name)
			}
		case StageGraduated, StageRemoved:
			if f.Message == "" {
				return nil, fmt.Errorf("flag %q: Message is required for %s flags", f.Name, f.Stage)
			}
		default:
			return nil, fmt.Errorf("flag %q: unknown stage %q", f.Name, f.Stage)
		}
		r.index[f.Name] = f
		r.ordered = append(r.ordered, f)
	}
	return r, nil
}

// Lookup returns the flag with the given name.
func (r *Registry) Lookup(name string) (Flag, bool) {
	f, ok := r.index[name]
	return f, ok
}

// All returns every entry in registration order.
func (r *Registry) All() []Flag {
	out := make([]Flag, len(r.ordered))
	copy(out, r.ordered)
	return out
}

// experimentalNames returns the sorted names of settable (experimental)
// flags, for "valid names are:" warning text.
func (r *Registry) experimentalNames() []string {
	var names []string
	for _, f := range r.ordered {
		if f.Stage == StageExperimental {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	return names
}

// builtin is gridctl's flag registry. Entries are append-only: graduation
// flips Stage and adds a Message, removal flips Stage again; names are never
// deleted. The lifecycle rules live in CONTRIBUTING.md.
var builtin = []Flag{
	{
		Name:        "transport_dual_stack",
		Description: "MCP 2026-07-28 transport dual-stack; always on, per-server pinning via protocol_generation.",
		Stage:       StageGraduated,
		Since:       "0.1.0",
		GraduatesBy: "0.3.0",
		Message:     "graduated in 0.1.0-rc.1; remove the entry, the dual-stack transport is always on and per-server pinning lives in protocol_generation",
	},
}

var defaultRegistry = sync.OnceValues(func() (*Registry, error) {
	return NewRegistry(builtin...)
})

// Default returns the built-in registry. The entries are compile-time
// constants validated by TestBuiltinRegistryValid; if construction ever
// fails at runtime an empty registry is returned so no flag resolves on.
func Default() *Registry {
	r, err := defaultRegistry()
	if err != nil {
		return &Registry{index: map[string]Flag{}}
	}
	return r
}

// Warning is one advisory finding from resolution or name validation.
// Warnings never block an apply.
type Warning struct {
	// Name is the flag key the warning is about ("" for none).
	Name string
	// Message is the full human-readable warning text.
	Message string
}

// Resolved is the outcome of resolving a stack's experimental map against a
// registry and the process environment.
type Resolved struct {
	// Enabled maps every known experimental flag name that resolved to true.
	// Flags resolving to false are absent. Nil when nothing is enabled.
	Enabled map[string]bool
	// Warnings lists unknown names, concluded names, and malformed env
	// overrides encountered during resolution.
	Warnings []Warning
}

// Resolve computes the effective flag set from the stack.yaml `experimental:`
// map and per-flag GRIDCTL_EXPERIMENTAL_<NAME> env overrides. An env override
// beats the YAML value; an unset env var defers to YAML; a malformed env
// value warns and defers to YAML (never silently false). Unknown YAML names
// warn listing the valid flags; graduated and removed names warn with their
// migration message and contribute nothing.
func Resolve(reg *Registry, experimental map[string]bool) Resolved {
	res := Resolved{}
	res.Warnings = append(res.Warnings, CheckNames(reg, experimental)...)

	values := map[string]bool{}
	for name, value := range experimental {
		if f, ok := reg.Lookup(name); ok && f.Stage == StageExperimental {
			values[name] = value
		}
	}
	for _, f := range reg.All() {
		if f.Stage != StageExperimental {
			continue
		}
		override, err := env.Bool(f.EnvVar())
		if err != nil {
			res.Warnings = append(res.Warnings, Warning{
				Name: f.Name,
				Message: fmt.Sprintf("%s=%q is not a boolean (accepted: 1, 0, true, false); ignoring the override",
					f.EnvVar(), os.Getenv(f.EnvVar())),
			})
			continue
		}
		if override != nil {
			values[f.Name] = *override
		}
	}

	for name, on := range values {
		if !on {
			continue
		}
		if res.Enabled == nil {
			res.Enabled = map[string]bool{}
		}
		res.Enabled[name] = true
	}
	return res
}

// CheckNames validates the names of a stack's experimental map against a
// registry without consulting the environment. It backs both apply-time
// console warnings and the design-time ValidateWithIssues channel.
func CheckNames(reg *Registry, experimental map[string]bool) []Warning {
	if len(experimental) == 0 {
		return nil
	}
	names := make([]string, 0, len(experimental))
	for name := range experimental {
		names = append(names, name)
	}
	sort.Strings(names)

	var warnings []Warning
	for _, name := range names {
		f, ok := reg.Lookup(name)
		if !ok {
			valid := reg.experimentalNames()
			hint := "no experimental flags are registered in this build"
			if len(valid) > 0 {
				hint = "valid flags: " + strings.Join(valid, ", ")
			}
			warnings = append(warnings, Warning{
				Name:    name,
				Message: fmt.Sprintf("unknown experimental flag %q ignored (%s)", name, hint),
			})
			continue
		}
		switch f.Stage {
		case StageGraduated, StageRemoved:
			warnings = append(warnings, Warning{
				Name:    name,
				Message: fmt.Sprintf("experimental flag %q %s: %s", name, concludedVerb(f.Stage), f.Message),
			})
		case StageExperimental:
			// Settable; nothing to report.
		}
	}
	return warnings
}

// Overdue returns the experimental flags whose GraduatesBy deadline is at or
// before currentVersion. An empty, "dev", or unparseable currentVersion
// returns nil: development builds never enforce the graduation clock.
// Pre-release suffixes are ignored — the clock compares major.minor.patch
// only, so 0.3.0-beta.1 already counts as 0.3.0.
func Overdue(reg *Registry, currentVersion string) []Flag {
	current, ok := parseVersion(currentVersion)
	if !ok {
		return nil
	}
	var overdue []Flag
	for _, f := range reg.All() {
		if f.Stage != StageExperimental {
			continue
		}
		deadline, ok := parseVersion(f.GraduatesBy)
		if !ok {
			continue
		}
		if compareVersions(current, deadline) >= 0 {
			overdue = append(overdue, f)
		}
	}
	return overdue
}

// concludedVerb labels a concluded flag's warning. The Message is expected
// to name the release the conclusion landed in (see CONTRIBUTING.md) — the
// registry cannot derive it, since GraduatesBy is a deadline, not the
// release the decision actually shipped in.
func concludedVerb(s Stage) string {
	if s == StageGraduated {
		return "graduated"
	}
	return "was removed"
}

type version struct{ major, minor, patch uint64 }

// parseVersion parses "1.2.3", "v1.2.3", or "1.2.3-beta.4" into a numeric
// triple, deliberately dropping any pre-release suffix: the graduation clock
// treats 0.3.0-beta.1 as 0.3.0 so a decision is forced by the first build of
// the deadline release.
func parseVersion(s string) (version, bool) {
	v, err := semver.NewVersion(strings.TrimSpace(s))
	if err != nil {
		return version{}, false
	}
	return version{major: v.Major(), minor: v.Minor(), patch: v.Patch()}, true
}

func compareVersions(a, b version) int {
	switch {
	case a.major != b.major:
		return cmpUint64(a.major, b.major)
	case a.minor != b.minor:
		return cmpUint64(a.minor, b.minor)
	default:
		return cmpUint64(a.patch, b.patch)
	}
}

func cmpUint64(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
