// Package servercard builds, validates, and fetches SEP-2127 Server Cards
// and the draft AI Catalog document that points at them.
//
// Cards are advisory public metadata. They are not a trust decision, a
// credential source, or a substitute for the gateway's own pin flow.
package servercard

// Media types and the schema URL the card document must declare.
const (
	MediaTypeServerCard = "application/mcp-server-card+json"
	MediaTypeCatalog    = "application/ai-catalog+json"
	SchemaURL           = "https://static.modelcontextprotocol.io/schemas/v1/server-card.schema.json"
	ServerCardSuffix    = "/server-card"
	CatalogWellKnown    = "/.well-known/ai-catalog.json"
	MaxBodyBytes        = 64 * 1024
)

// ServerCard is a static discovery document for one remote MCP server.
// Field names follow the vendored schema. Tools, prompts, and resources
// are intentionally absent.
type ServerCard struct {
	Schema      string         `json:"$schema"`
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Remotes     []Remote       `json:"remotes,omitempty"`
	Meta        map[string]any `json:"_meta,omitempty"`
}

// Remote is one HTTP endpoint a client can connect to.
type Remote struct {
	Type                      string          `json:"type"`
	URL                       string          `json:"url"`
	Headers                   []KeyValueInput `json:"headers,omitempty"`
	SupportedProtocolVersions []string        `json:"supportedProtocolVersions,omitempty"`
}

// KeyValueInput is a named header whose value may reference variables.
type KeyValueInput struct {
	Name        string           `json:"name"`
	Value       string           `json:"value,omitempty"`
	Description string           `json:"description,omitempty"`
	IsRequired  bool             `json:"isRequired,omitempty"`
	IsSecret    bool             `json:"isSecret,omitempty"`
	Variables   map[string]Input `json:"variables,omitempty"`
}

// Input describes one user-supplied or pre-set value.
type Input struct {
	Description string `json:"description,omitempty"`
	Value       string `json:"value,omitempty"`
	IsRequired  bool   `json:"isRequired,omitempty"`
	IsSecret    bool   `json:"isSecret,omitempty"`
}

// Catalog is an AI Catalog document. specVersion 1.0 allows an empty
// entries array.
type Catalog struct {
	SpecVersion string         `json:"specVersion"`
	Entries     []CatalogEntry `json:"entries"`
}

// CatalogEntry points at one Server Card.
type CatalogEntry struct {
	Identifier string `json:"identifier"`
	Type       string `json:"type"`
	URL        string `json:"url"`
}
