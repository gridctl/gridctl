package sandbox

import (
	"fmt"

	"github.com/evanw/esbuild/pkg/api"
)

// TranspileTS is the exported wrapper around the sandbox's TS
// transpile path. The compiled output is the same CommonJS shape the
// sandbox itself runs, so a CLI consumer (`gridctl agent build`) can
// pre-compile a skill and produce an artifact byte-equivalent to what
// the runtime would produce on first invocation.
func TranspileTS(source string) (string, error) { return transpileTS(source) }

// transpileTS converts a TypeScript source file to ES2015-compatible
// JavaScript that goja can execute. The output uses CommonJS module
// shape so the harness can read the skill's default export through
// `module.exports.default`. Bundling is intentionally disabled —
// skills are single-file in Phase C; multi-file skill packages can
// land in a follow-up once the on-disk shape settles.
//
// The transformer is deliberately strict: any TS error fails the
// transpile rather than silently emitting partial code, so authors
// see the typing problem at registration time.
func transpileTS(source string) (string, error) {
	result := api.Transform(source, api.TransformOptions{
		Loader:            api.LoaderTS,
		Target:            api.ES2015,
		Format:            api.FormatCommonJS,
		MinifySyntax:      false,
		MinifyWhitespace:  false,
		MinifyIdentifiers: false,
	})
	if len(result.Errors) > 0 {
		msg := result.Errors[0]
		loc := ""
		if msg.Location != nil {
			loc = fmt.Sprintf(" at line %d, column %d", msg.Location.Line, msg.Location.Column)
		}
		return "", fmt.Errorf("ts transpile failed%s: %s", loc, msg.Text)
	}
	return string(result.Code), nil
}
