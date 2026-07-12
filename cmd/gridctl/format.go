package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// resolveFormat merges the legacy --format flag with the boolean --json
// alias. Passing --json is equivalent to --format json; explicitly combining
// --json with a different --format value is an error. formatChanged reports
// whether the user set --format (defaults never conflict with --json).
func resolveFormat(format string, formatChanged, asJSON bool) (string, error) {
	if !asJSON {
		return format, nil
	}
	if formatChanged && !strings.EqualFold(format, "json") {
		return "", fmt.Errorf("cannot combine --json with --format=%s", format)
	}
	return "json", nil
}

// addJSONAlias registers a --json boolean alias next to an existing
// --format flag on cmd. The returned pointer reports whether it was set.
func addJSONAlias(cmd *cobra.Command) *bool {
	var asJSON bool
	cmd.Flags().BoolVar(&asJSON, "json", false, "Shorthand for --format json")
	return &asJSON
}
