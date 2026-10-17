package servercard

import (
	"fmt"
	"os"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// CompileVendoredSchema loads the vendored SEP-2127 schema and compiles
// #/$defs/ServerCard. Production Validate does not call this. Tests and the
// integration suite use it so the schema library stays a direct dependency
// and the check does not fetch a schema over the network.
func CompileVendoredSchema(path string) (*jsonschema.Schema, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	const loc = "https://gridctl.local/server-card.schema.json"
	if err := compiler.AddResource(loc, doc); err != nil {
		return nil, err
	}
	schema, err := compiler.Compile(loc + "#/$defs/ServerCard")
	if err != nil {
		return nil, fmt.Errorf("compile server card schema: %w", err)
	}
	return schema, nil
}
