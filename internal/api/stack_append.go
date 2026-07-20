package api

import (
	"fmt"

	"github.com/gridctl/gridctl/internal/stackedit"
)

// resourceTypeToKey maps the request's `resourceType` field to the top-level
// stack-YAML key the new entry is appended under.
var resourceTypeToKey = map[string]string{
	"mcp-server": "mcp-servers",
	"resource":   "resources",
}

// patchAppendResource appends a single resource to the appropriate top-level
// sequence in source via stackedit's comment-preserving yaml.Node round-trip.
//
// resourceType selects the target sequence ("mcp-server" → mcp-servers,
// "resource" → resources). snippet is the YAML body of the entry being
// appended; it must parse to a single mapping.
func patchAppendResource(source []byte, resourceType string, snippet []byte) ([]byte, error) {
	key, ok := resourceTypeToKey[resourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported resourceType: %s", resourceType)
	}
	return stackedit.AppendResources(source, key, snippet)
}
