package config

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"
)

// RequiresConfig is the optional top-level requires block. Omitted means
// no constraint (Article IX). A booster fragment cannot carry it; the
// fragment allowlist rejects the key before decode.
type RequiresConfig struct {
	// Gridctl is a semver constraint the running gridctl must satisfy.
	// Empty means no constraint.
	Gridctl string `yaml:"gridctl,omitempty" json:"gridctl,omitempty"`
}

var (
	runningMu        sync.Mutex
	runningVersion   string
	requiresSkipOnce sync.Once
)

// simpleGTE matches one >= clause. The upgrade hint uses the highest such
// clause when the constraint has no "||" and that version satisfies the
// whole constraint.
var simpleGTE = regexp.MustCompile(`^>=\s*v?(\S+)\s*$`)

// SetRunningVersion records the gridctl version used by requires.gridctl
// checks. cmd/gridctl calls it once from PersistentPreRunE with the ldflags
// version. The daemon child re-enters main, so that one call covers the
// controller. An empty value, "dev", or any string that is not a semver
// skips satisfaction checks after the constraint string is syntax-checked.
func SetRunningVersion(v string) {
	runningMu.Lock()
	runningVersion = strings.TrimSpace(v)
	runningMu.Unlock()
}

// ResetRunningVersion clears the recorded version and the once-per-process
// skip warning. Tests call it from t.Cleanup. Requires checks share this
// process-wide state, so those tests must not run in parallel with each other.
func ResetRunningVersion() {
	runningMu.Lock()
	runningVersion = ""
	requiresSkipOnce = sync.Once{}
	runningMu.Unlock()
}

func currentRunningVersion() string {
	runningMu.Lock()
	defer runningMu.Unlock()
	return runningVersion
}

// RequiresError is a requires.gridctl failure. Syntax is true when the
// constraint string does not parse. Otherwise the running version does not
// satisfy Constraint. Error returns the stack validation message. Pack
// callers format their own text from the fields.
type RequiresError struct {
	Constraint string
	Running    string
	// Floor is the highest >= clause that satisfies Constraint, with no v
	// prefix. Empty when Constraint uses ||, or when no such clause exists.
	Floor  string
	Syntax bool
}

func (e *RequiresError) Error() string {
	if e == nil {
		return ""
	}
	if e.Syntax {
		return `must be a semver constraint (e.g. ">=1.1.0")`
	}
	msg := fmt.Sprintf("stack requires gridctl %s, running %s", e.Constraint, e.Running)
	if e.Floor != "" {
		msg += " (gridctl upgrade --version v" + strings.TrimPrefix(e.Floor, "v") + ")"
	}
	return msg
}

// CheckRequires reports whether the running version satisfies constraint.
// An empty constraint is not a constraint. A string that does not parse is
// an error even when the running version is not a release. Satisfaction is
// skipped for a dev, empty, or non-semver running version, and one warning
// is logged per process.
func CheckRequires(constraint string) error {
	return checkRequires(currentRunningVersion(), constraint)
}

func checkRequires(running, constraint string) error {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil
	}
	parsed, err := semver.NewConstraint(constraint)
	if err != nil {
		return &RequiresError{Constraint: constraint, Syntax: true}
	}
	// Pre-release versions are excluded from ranges unless this is set.
	// The current release line is a release candidate.
	parsed.IncludePrerelease = true

	display, release := releaseVersion(running)
	if !release {
		warnRequiresSkipped(running)
		return nil
	}
	version, err := semver.NewVersion(running)
	if err != nil {
		warnRequiresSkipped(running)
		return nil
	}
	if parsed.Check(version) {
		return nil
	}
	return &RequiresError{
		Constraint: constraint,
		Running:    display,
		Floor:      simpleFloor(constraint),
	}
}

// releaseVersion reports the display form of a release version. dev, empty,
// and unparsable strings are not releases. A leading v is stripped in the
// display form so the message matches the documented running version.
func releaseVersion(running string) (string, bool) {
	running = strings.TrimSpace(running)
	if running == "" || running == "dev" {
		return running, false
	}
	version, err := semver.NewVersion(running)
	if err != nil {
		return running, false
	}
	return version.String(), true
}

func warnRequiresSkipped(running string) {
	requiresSkipOnce.Do(func() {
		slog.Warn(fmt.Sprintf("requires.gridctl not enforced: running version %q is not a release", running))
	})
}

// simpleFloor returns the highest >= clause that satisfies constraint,
// without a v prefix. A "||" constraint has no floor hint, and neither does
// an AND whose other clauses exclude that version.
func simpleFloor(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || strings.Contains(constraint, "||") {
		return ""
	}
	parsed, err := semver.NewConstraint(constraint)
	if err != nil {
		return ""
	}
	parsed.IncludePrerelease = true
	var best *semver.Version
	for _, clause := range strings.Split(constraint, ",") {
		match := simpleGTE.FindStringSubmatch(strings.TrimSpace(clause))
		if match == nil {
			continue
		}
		version, err := semver.NewVersion(match[1])
		if err != nil {
			continue
		}
		if best == nil || version.GreaterThan(best) {
			best = version
		}
	}
	if best == nil || !parsed.Check(best) {
		return ""
	}
	return best.String()
}

// intersectConstraints encodes child AND parent as one Masterminds string.
// "," binds tighter than "||", so joining the two strings with ", " is not
// an intersection when either side uses "||": ">=1.0.0 || >=2.0.0, >=1.5.0"
// accepts 1.2.0. The cross product is the OR of each child clause AND each
// parent clause, which is the intersection. Two simple floors still join as
// "<child>, <parent>". A side that does not parse is comma-joined so the
// syntax error is preserved. An intersection Masterminds cannot represent
// (too long, or too many OR groups) is still returned and fails closed at
// check time; falling back to a comma join would drop a floor.
func intersectConstraints(child, parent string) string {
	if _, err := semver.NewConstraint(child); err != nil {
		return child + ", " + parent
	}
	if _, err := semver.NewConstraint(parent); err != nil {
		return child + ", " + parent
	}
	childGroups := splitOrGroups(child)
	parentGroups := splitOrGroups(parent)
	pairs := make([]string, 0, len(childGroups)*len(parentGroups))
	for _, c := range childGroups {
		for _, p := range parentGroups {
			pairs = append(pairs, c+", "+p)
		}
	}
	return strings.Join(pairs, " || ")
}

// splitOrGroups splits on "||", the same split NewConstraint uses, and
// trims each group. Empty groups are kept so an invalid constraint is not
// rewritten into a valid one.
func splitOrGroups(constraint string) []string {
	parts := strings.Split(constraint, "||")
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = strings.TrimSpace(part)
	}
	return out
}
