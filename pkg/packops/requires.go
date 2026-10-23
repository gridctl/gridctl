package packops

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/pack"
)

// packRequiresConstraint returns the manifest's requires.gridctl string.
// Whitespace-only is no constraint.
func packRequiresConstraint(m *pack.Manifest) string {
	if m == nil || m.Requires == nil {
		return ""
	}
	return strings.TrimSpace(m.Requires.Gridctl)
}

// enforcePackRequires checks constraint against the running gridctl.
// An empty constraint is not a constraint. The error names the pack and
// unwraps to ErrPackRequires.
func enforcePackRequires(name, constraint string) error {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || name == "" {
		return nil
	}
	err := config.CheckRequires(constraint)
	if err == nil {
		return nil
	}
	var re *config.RequiresError
	if errors.As(err, &re) && !re.Syntax {
		return &packError{
			reason: ErrPackRequires,
			msg:    fmt.Sprintf("pack %s requires gridctl %s, running %s", name, re.Constraint, re.Running),
		}
	}
	return &packError{
		reason: ErrPackRequires,
		msg:    fmt.Sprintf("pack %s: requires.gridctl: %s", name, err.Error()),
	}
}
