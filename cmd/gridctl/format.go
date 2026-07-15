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

// addPlainFlag registers the --plain table flag on cmd. The returned
// pointer reports whether it was set.
func addPlainFlag(cmd *cobra.Command) *bool {
	var plain bool
	cmd.Flags().BoolVar(&plain, "plain", false, "Render tables without box-drawing (grep/awk friendly)")
	return &plain
}

// resolvePlain validates --plain against the resolved output format. Plain
// tables and JSON are different consumers; combining them is a mistake.
func resolvePlain(plain bool, format string) error {
	if plain && strings.EqualFold(format, "json") {
		return fmt.Errorf("cannot combine --plain with --json (or --format=json)")
	}
	return nil
}
