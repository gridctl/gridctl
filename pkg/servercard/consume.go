package servercard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/gridctl/gridctl/pkg/catalog"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
)

var (
	bearerValuePattern = regexp.MustCompile(`^Bearer \{([A-Za-z_][A-Za-z0-9_]*)\}$`)
	singleValuePattern = regexp.MustCompile(`^\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
	placeholderPattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// Draft is a catalog entry synthesized from a card, plus warnings that
// did not become stack fields. ProtocolGeneration is empty unless every
// listed version is supported and they share one era.
type Draft struct {
	Entry              catalog.Entry
	Warnings           []string
	ProtocolGeneration string
	CardURL            string
}

// DraftFromCard maps one same-origin remote onto a catalog entry the
// existing add path can prompt, vault, and write. The submitted URL is
// the caller's origin; an off-origin remote or a URL template is refused
// rather than followed.
func DraftFromCard(card *ServerCard, submittedURL string) (Draft, error) {
	if card == nil {
		return Draft{}, ErrNoCard
	}
	if err := Validate(card); err != nil {
		return Draft{}, fmt.Errorf("%w: %v", ErrInvalidCard, err)
	}
	remote, err := selectRemote(card, submittedURL)
	if err != nil {
		return Draft{}, err
	}
	transport := "http"
	if remote.Type == "sse" {
		transport = "sse"
	}
	entry := catalog.Entry{
		Name:        card.Name,
		Title:       card.Title,
		Description: card.Description,
		Tier:        "url",
		Install: catalog.Install{
			Type:      catalog.InstallURL,
			Transport: transport,
			URL:       remote.URL,
		},
	}
	warnings, inputs, authType, authHeader, authVar := mapHeaders(remote.Headers)
	if authType != "" {
		entry.Install.AuthType = authType
		entry.Install.AuthHeader = authHeader
		for i := range inputs {
			if inputs[i].Name == authVar {
				inputs[i].Auth = true
			}
		}
	}
	entry.Inputs = inputs
	return Draft{
		Entry:              entry,
		Warnings:           warnings,
		ProtocolGeneration: protocolGeneration(remote.SupportedProtocolVersions),
		CardURL:            remote.URL,
	}, nil
}

// ServerDraft fills required inputs with ${var:KEY} placeholders and
// returns the stack block. It never copies a credential value.
func (d Draft) ServerDraft(name string) (config.MCPServer, []string, error) {
	if name == "" {
		name = d.Entry.ServerName()
	}
	if name == "" {
		name = "server"
	}
	values := make(map[string]string, len(d.Entry.Inputs))
	for _, in := range d.Entry.Inputs {
		if in.Required || in.Auth {
			values[in.Name] = "${var:" + catalog.VarKey(in.Name) + "}"
		}
	}
	server, warnings, err := d.Entry.Server(name, values)
	if err != nil {
		return config.MCPServer{}, nil, err
	}
	if d.ProtocolGeneration != "" {
		server.ProtocolGeneration = d.ProtocolGeneration
	}
	return server, append(append([]string{}, d.Warnings...), warnings...), nil
}

func selectRemote(card *ServerCard, submittedURL string) (Remote, error) {
	if len(card.Remotes) == 0 {
		return Remote{}, fmt.Errorf("%w: card has no remotes", ErrNoCard)
	}
	remote := card.Remotes[0]
	if strings.Contains(remote.URL, "{") {
		return Remote{}, fmt.Errorf("%w: %s", ErrTemplateURL, remote.URL)
	}
	if !SameOrigin(remote.URL, submittedURL) {
		return Remote{}, fmt.Errorf("%w: remote %s is not the same origin as %s", ErrOffOrigin, remote.URL, submittedURL)
	}
	return remote, nil
}

// mapHeaders derives one input per variable. A simple Authorization
// "Bearer {x}" or a single "{x}" header becomes the auth block. Any
// further required header, or a literal value, is a warning and is not
// written (headers: is not a stack field yet).
func mapHeaders(headers []KeyValueInput) (warnings []string, inputs []catalog.Input, authType, authHeader, authVar string) {
	simple, simpleVar := simpleAuth(headers)
	for i, header := range headers {
		headerInputs, headerWarnings := inputsForHeader(header)
		warnings = append(warnings, headerWarnings...)
		useAuth := simple.authType != "" && i == 0
		if !useAuth && (header.IsRequired || len(headerInputs) > 0) {
			warnings = append(warnings, fmt.Sprintf("header %s was not written; additional headers are not a stack field yet", header.Name))
			continue
		}
		if useAuth {
			authType, authHeader, authVar = simple.authType, simple.header, simpleVar
		}
		inputs = append(inputs, headerInputs...)
	}
	return warnings, inputs, authType, authHeader, authVar
}

type simpleAuthKind struct {
	authType string
	header   string
}

func simpleAuth(headers []KeyValueInput) (simpleAuthKind, string) {
	if len(headers) != 1 {
		return simpleAuthKind{}, ""
	}
	header := headers[0]
	if strings.EqualFold(header.Name, "Authorization") {
		if m := bearerValuePattern.FindStringSubmatch(header.Value); m != nil && hasVariable(header, m[1]) {
			return simpleAuthKind{authType: "bearer"}, m[1]
		}
	}
	if m := singleValuePattern.FindStringSubmatch(header.Value); m != nil && hasVariable(header, m[1]) {
		return simpleAuthKind{authType: "header", header: header.Name}, m[1]
	}
	return simpleAuthKind{}, ""
}

func hasVariable(header KeyValueInput, name string) bool {
	if len(header.Variables) == 0 {
		return true
	}
	_, ok := header.Variables[name]
	return ok
}

func inputsForHeader(header KeyValueInput) ([]catalog.Input, []string) {
	var warnings []string
	var inputs []catalog.Input
	if len(header.Variables) == 0 {
		seen := map[string]bool{}
		for _, match := range placeholderPattern.FindAllStringSubmatch(header.Value, -1) {
			name := match[1]
			if seen[name] {
				continue
			}
			seen[name] = true
			warnings = append(warnings, fmt.Sprintf("header %s placeholder {%s} has no variable; treating it as a required secret", header.Name, name))
			inputs = append(inputs, catalog.Input{
				Name:        name,
				Description: header.Description,
				Required:    true,
				Secret:      true,
			})
		}
		if len(inputs) == 0 && header.Value != "" {
			warnings = append(warnings, fmt.Sprintf("header %s has a literal value and was not written", header.Name))
		}
		return inputs, warnings
	}
	names := make([]string, 0, len(header.Variables))
	for name := range header.Variables {
		names = append(names, name)
	}
	// Stable order so tests and prompts do not depend on map iteration.
	sortStrings(names)
	for _, name := range names {
		variable := header.Variables[name]
		required := variable.IsRequired
		secret := variable.IsSecret
		if !variable.IsRequired && !variable.IsSecret && header.IsRequired {
			required = header.IsRequired
			secret = header.IsSecret
		}
		inputs = append(inputs, catalog.Input{
			Name:        name,
			Description: variable.Description,
			Required:    required,
			Secret:      secret,
		})
	}
	for _, match := range placeholderPattern.FindAllStringSubmatch(header.Value, -1) {
		if _, ok := header.Variables[match[1]]; !ok {
			warnings = append(warnings, fmt.Sprintf("header %s placeholder {%s} has no variable; treating it as a required secret", header.Name, match[1]))
			inputs = append(inputs, catalog.Input{Name: match[1], Required: true, Secret: true})
		}
	}
	return inputs, warnings
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func protocolGeneration(versions []string) string {
	if len(versions) == 0 {
		return ""
	}
	var era mcp.ProtocolEra
	for i, version := range versions {
		if !mcp.IsSupportedProtocolVersion(version) {
			return ""
		}
		next := mcp.EraOfVersion(version)
		if i == 0 {
			era = next
			continue
		}
		if next != era {
			return ""
		}
	}
	return string(era)
}
