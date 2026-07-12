package output

import (
	"io"
	"os"
)

// noColor is the process-wide color kill switch, set by the global
// --no-color flag before command output is produced.
var noColor bool

// SetNoColor disables (or re-enables) all styled output for the process,
// regardless of TTY detection. Wired to the global --no-color flag.
func SetNoColor(disabled bool) {
	noColor = disabled
}

// ColorEnabled reports whether styled output should be emitted on w.
// Color is disabled when SetNoColor(true) was called, NO_COLOR is set and
// non-empty (https://no-color.org/), TERM is "dumb", or w is not a terminal.
func ColorEnabled(w io.Writer) bool {
	return colorAllowedByEnv() && isTerminal(w)
}

// colorAllowedByEnv applies every color gate that does not depend on the
// output stream: the --no-color kill switch, NO_COLOR, and TERM=dumb.
func colorAllowedByEnv() bool {
	if noColor {
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return true
}
