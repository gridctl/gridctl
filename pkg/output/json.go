package output

import (
	"encoding/json"
	"io"
)

// EncodeJSON writes v to w as two-space-indented JSON. It is the shared
// encoder for every command's machine-readable output so schemas render
// consistently and never carry ANSI escapes.
func EncodeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
