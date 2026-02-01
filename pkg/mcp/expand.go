package mcp

import (
	"os"
	"regexp"
)

// envVarRegex matches POSIX-style environment variable patterns:
//   - ${VAR}              - simple variable reference
//   - ${VAR:-default}     - use default if undefined or empty
//   - ${VAR:+replacement} - use replacement if defined and non-empty
//
// Variable names: start with letter or underscore, followed by letters, digits, or underscores.
// Operand can contain any characters except closing brace.
var envVarRegex = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?::([+-])([^}]*))?\}`)

// expandEnvVars performs POSIX-style environment variable expansion on data.
// Undefined variables expand to empty string (POSIX standard).
func expandEnvVars(data []byte) []byte {
	return envVarRegex.ReplaceAllFunc(data, func(match []byte) []byte {
		parts := envVarRegex.FindSubmatch(match)
		// Defensive check - should always have 4 capture groups (full match + 3 groups)
		if len(parts) < 4 {
			return match
		}

		varName := string(parts[1])
		value, exists := os.LookupEnv(varName)

		// Simple ${VAR} case - no operator
		if len(parts[2]) == 0 {
			return []byte(value)
		}

		op := parts[2][0]
		operand := string(parts[3])

		switch op {
		case '-':
			// ${VAR:-default} - use default if VAR is undefined or empty
			if value == "" {
				return []byte(operand)
			}
			return []byte(value)
		case '+':
			// ${VAR:+replacement} - use replacement if VAR is defined and non-empty
			if exists && value != "" {
				return []byte(operand)
			}
			return []byte{}
		default:
			// Unknown operator, return as-is
			return match
		}
	})
}
