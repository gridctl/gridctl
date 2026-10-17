package servercard

import (
	"strings"
	"unicode/utf8"
)

const (
	nameNamespace = "io.gridctl"
	nameMax       = 200
	textMax       = 100
)

// Identity is the gateway identity a card is built from. It never carries
// a credential value.
type Identity struct {
	// GatewayName is the serverInfo.name the gateway announces, before any
	// group suffix.
	GatewayName string
	// StackName is the stack file name used in the default description.
	StackName string
	// Description overrides the default when non-empty.
	Description string
	// Version is the gateway version string.
	Version string
	// Group is empty for the gateway card.
	Group string
	// AuthType is "bearer", "api_key", or empty.
	AuthType string
	// AuthHeader is the api_key header name. Empty means Authorization.
	AuthHeader string
	// Origin is scheme://host[:port]. Empty omits remotes.
	Origin string
	// PathPrefix is the public_url path, without a trailing slash.
	PathPrefix string
	// ProtocolVersions is copied onto the remote when an origin exists.
	ProtocolVersions []string
}

// Build returns a card that Validate accepts. A gateway name with a slash,
// a space, or a long value is sanitized before it reaches name.
func Build(id Identity) (*ServerCard, error) {
	segment := sanitizeSegment(id.GatewayName)
	groupSeg := ""
	if id.Group != "" {
		groupSeg = sanitizeSegment(id.Group)
	}
	title := id.GatewayName
	if title == "" {
		title = "gridctl-gateway"
	}
	if id.Group != "" {
		title = title + "/" + id.Group
	}
	desc := id.Description
	if desc == "" {
		stack := id.StackName
		if stack == "" {
			stack = "gridctl"
		}
		desc = "MCP gateway for stack " + stack
	}
	version := id.Version
	if version == "" {
		version = "dev"
	}
	card := &ServerCard{
		Schema:      SchemaURL,
		Name:        cardName(segment, groupSeg),
		Title:       truncateRunes(title, textMax),
		Description: truncateRunes(desc, textMax),
		Version:     truncateBytes(version, 255),
	}
	if card.Title == "" {
		card.Title = "gridctl"
	}
	if card.Description == "" {
		card.Description = "MCP gateway"
	}
	if id.Group != "" {
		card.Meta = map[string]any{"io.gridctl/group": id.Group}
	}
	if id.Origin != "" {
		path := joinPrefix(id.PathPrefix, mcpPath(id.Group))
		card.Remotes = []Remote{{
			Type:                      "streamable-http",
			URL:                       strings.TrimRight(id.Origin, "/") + path,
			SupportedProtocolVersions: append([]string(nil), id.ProtocolVersions...),
			Headers:                   authHeaders(id.AuthType, id.AuthHeader),
		}}
	}
	if err := Validate(card); err != nil {
		return nil, err
	}
	return card, nil
}

// BuildCatalog returns a catalog whose entries use the same origin as the
// cards. An empty origin yields an empty entries array.
func BuildCatalog(origin, prefix, segment string, groups []string, cards map[string]string) *Catalog {
	cat := &Catalog{SpecVersion: "1.0", Entries: []CatalogEntry{}}
	if origin == "" {
		return cat
	}
	host := hostname(origin)
	if host == "" {
		return cat
	}
	add := func(group, cardURL string) {
		ident := segment
		if group != "" {
			ident = segment + "." + sanitizeSegment(group)
		}
		cat.Entries = append(cat.Entries, CatalogEntry{
			Identifier: "urn:air:" + host + ":mcp:" + ident,
			Type:       MediaTypeServerCard,
			URL:        cardURL,
		})
	}
	add("", cards[""])
	for _, group := range groups {
		add(group, cards[group])
	}
	return cat
}

// CardURL builds the absolute card URL for an origin and optional group.
func CardURL(origin, prefix, group string) string {
	return strings.TrimRight(origin, "/") + joinPrefix(prefix, cardPath(group))
}

func cardPath(group string) string {
	if group == "" {
		return "/mcp/server-card"
	}
	return "/groups/" + group + "/mcp/server-card"
}

func mcpPath(group string) string {
	if group == "" {
		return "/mcp"
	}
	return "/groups/" + group + "/mcp"
}

func joinPrefix(prefix, path string) string {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "/" {
		prefix = ""
	}
	return prefix + path
}

func cardName(segment, group string) string {
	second := segment
	if group != "" {
		second = segment + "." + group
	}
	const head = nameNamespace + "/"
	name := head + second
	if len(name) <= nameMax {
		return name
	}
	budget := nameMax - len(head)
	if budget < 1 {
		return head + "gateway"
	}
	second = second[:budget]
	second = strings.TrimRight(second, "-.")
	if second == "" {
		second = "gateway"
	}
	return head + second
}

// Segment returns the sanitized gateway-name segment used in card names
// and catalog identifiers. Dots are preserved. A group suffix is not appended.
func Segment(name string) string {
	return sanitizeSegment(name)
}

// sanitizeSegment maps a gateway or group name onto the schema's second
// segment. Characters outside [a-zA-Z0-9._-] become "-", runs of "-"
// collapse, and leading or trailing "-" and "." are trimmed. An empty
// result becomes "gateway".
func sanitizeSegment(raw string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range raw {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			r = '-'
		}
		if r == '-' {
			if prevDash {
				continue
			}
			prevDash = true
			b.WriteByte('-')
			continue
		}
		prevDash = false
		b.WriteRune(r)
	}
	s := strings.Trim(b.String(), "-.")
	if s == "" {
		return "gateway"
	}
	return s
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func hostname(origin string) string {
	rest := origin
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	if i := strings.IndexAny(rest, "/"); i >= 0 {
		rest = rest[:i]
	}
	if strings.HasPrefix(rest, "[") {
		if j := strings.Index(rest, "]"); j >= 0 {
			return rest[1:j]
		}
	}
	if h, _, ok := strings.Cut(rest, ":"); ok {
		return h
	}
	return rest
}

func authHeaders(authType, headerName string) []KeyValueInput {
	switch authType {
	case "bearer":
		return []KeyValueInput{{
			Name:       "Authorization",
			Value:      "Bearer {token}",
			IsRequired: true,
			IsSecret:   true,
			Variables: map[string]Input{
				"token": {
					Description: "Gateway bearer token",
					IsRequired:  true,
					IsSecret:    true,
				},
			},
		}}
	case "api_key":
		name := headerName
		if name == "" {
			name = "Authorization"
		}
		return []KeyValueInput{{
			Name:       name,
			Value:      "{key}",
			IsRequired: true,
			IsSecret:   true,
			Variables: map[string]Input{
				"key": {
					Description: "Gateway API key",
					IsRequired:  true,
					IsSecret:    true,
				},
			},
		}}
	default:
		return nil
	}
}
