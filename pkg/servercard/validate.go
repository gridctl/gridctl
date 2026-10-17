package servercard

import (
	"fmt"
	"regexp"
	"unicode/utf8"
)

var (
	namePattern      = regexp.MustCompile(`^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$`)
	remoteURLPattern = regexp.MustCompile(`^(https?://[^\s]+|\{[a-zA-Z_][a-zA-Z0-9_]*\}[^\s]*)$`)
)

// Validate enforces the vendored schema's required fields, name pattern,
// and length bounds. It does not load a JSON Schema library.
func Validate(card *ServerCard) error {
	if card == nil {
		return fmt.Errorf("server card is nil")
	}
	if card.Schema != SchemaURL {
		return fmt.Errorf("server card $schema must be %s", SchemaURL)
	}
	if n := len(card.Name); n < 3 || n > 200 || !namePattern.MatchString(card.Name) {
		return fmt.Errorf("server card name %q does not match %s", card.Name, namePattern.String())
	}
	if n := utf8.RuneCountInString(card.Description); n < 1 || n > 100 {
		return fmt.Errorf("server card description must be 1 to 100 characters")
	}
	if card.Version == "" || len(card.Version) > 255 {
		return fmt.Errorf("server card version must be 1 to 255 characters")
	}
	if card.Title != "" {
		if n := utf8.RuneCountInString(card.Title); n < 1 || n > 100 {
			return fmt.Errorf("server card title must be 1 to 100 characters")
		}
	}
	for i, remote := range card.Remotes {
		if remote.Type != "sse" && remote.Type != "streamable-http" {
			return fmt.Errorf("server card remotes[%d].type %q is not sse or streamable-http", i, remote.Type)
		}
		if !remoteURLPattern.MatchString(remote.URL) {
			return fmt.Errorf("server card remotes[%d].url is invalid", i)
		}
		for j, header := range remote.Headers {
			if header.Name == "" {
				return fmt.Errorf("server card remotes[%d].headers[%d].name is required", i, j)
			}
		}
	}
	return nil
}
