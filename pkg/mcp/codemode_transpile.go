package mcp

import (
	"fmt"

	"github.com/evanw/esbuild/pkg/api"
)

// Transpile converts modern JavaScript (ES2020+) to ES2015 compatible code
// that can be executed by the goja runtime. It handles async/await, arrow
// functions, destructuring, template literals, and other modern syntax.
func Transpile(code string) (string, error) {
	result := api.Transform(code, api.TransformOptions{
		Target:            api.ES2015,
		Format:            api.FormatDefault,
		Loader:            api.LoaderJS,
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
		return "", fmt.Errorf("syntax error%s: %s", loc, msg.Text)
	}

	return string(result.Code), nil
}
