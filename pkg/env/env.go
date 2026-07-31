// Package env provides the one boolean environment-variable parsing rule
// used across gridctl. Boolean env vars (kill switches, experimental flag
// overrides) accept exactly the strconv.ParseBool vocabulary: 1, t, T, TRUE,
// true, True, 0, f, F, FALSE, false, False. Anything else is a parse error
// the caller surfaces as a warning; it never silently means false.
package env

import (
	"os"
	"strconv"
)

// Bool reads a boolean environment variable. The three outcomes are
// distinct on purpose:
//
//   - unset or empty: (nil, nil) — the variable expresses no opinion and the
//     caller falls back to its config-derived value
//   - parseable: (&value, nil)
//   - anything else: (nil, error) — the caller should warn and fall back,
//     never treat the malformed value as false
func Bool(name string) (*bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, err
	}
	return &v, nil
}
