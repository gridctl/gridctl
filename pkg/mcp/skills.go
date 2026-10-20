package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
)

var (
	errSkillsUnavailable = errors.New("skills extension unavailable")
	errSkillCursor       = errors.New("invalid cursor")
)

func (g *Gateway) SetSkillProvider(provider SkillProvider) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.skillsProvider = provider
}

func (g *Gateway) skillProvider() SkillProvider {
	if g.promptProvider() == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.skillsProvider
}

// DeclaredExtensions reports the extensions available to stateless clients.
func (g *Gateway) DeclaredExtensions() []string {
	var extensions []string
	if g.skillProvider() != nil {
		extensions = append(extensions, SkillsExtensionID)
	}
	if g.taskCapableServerName() != "" {
		extensions = append(extensions, TasksExtensionID)
	}
	sort.Strings(extensions)
	return extensions
}

type SkillsListResult struct {
	StatelessResultFields
	Skills []SkillEntry `json:"skills"`
}

type SkillsGetResult struct {
	StatelessResultFields
	Skill SkillEntry `json:"skill"`
}

type ResourcesDirectoryResult struct {
	StatelessResultFields
	Resources []SkillDirectoryEntry `json:"resources"`
}

func (g *Gateway) HandleSkillsList(ctx context.Context, cursor string) (*SkillsListResult, error) {
	provider := g.skillProvider()
	if provider == nil {
		return nil, errSkillsUnavailable
	}
	if cursor != "" {
		return nil, errSkillCursor
	}
	entries, err := provider.ListSkillEntries(ctx)
	if err != nil {
		return nil, err
	}
	result := &SkillsListResult{Skills: make([]SkillEntry, 0, len(entries))}
	policy := g.CurrentSkillPolicy()
	for _, entry := range entries {
		if policy.Allows(entry.Name) {
			result.Skills = append(result.Skills, entry)
		}
	}
	sort.Slice(result.Skills, func(i, j int) bool { return result.Skills[i].Name < result.Skills[j].Name })
	attachSkillCacheMeta(&result.StatelessResultFields)
	return result, nil
}

func (g *Gateway) HandleSkillsGet(ctx context.Context, uri string) (*SkillsGetResult, error) {
	provider := g.skillProvider()
	if provider == nil {
		return nil, errSkillsUnavailable
	}
	name, path, ok := parseSkillURI(uri)
	if !ok || path != "SKILL.md" || !g.CurrentSkillPolicy().Allows(name) {
		return nil, notFound(uri, "No skill is served at "+uri)
	}
	entry, err := provider.GetSkillEntry(ctx, name)
	if errors.Is(err, ErrSkillNotFound) {
		return nil, notFound(uri, "No skill is served at "+uri)
	}
	if err != nil {
		return nil, err
	}
	result := &SkillsGetResult{Skill: *entry}
	attachSkillCacheMeta(&result.StatelessResultFields)
	return result, nil
}

func (g *Gateway) HandleResourcesDirectoryRead(ctx context.Context, uri, cursor string) (*ResourcesDirectoryResult, error) {
	provider := g.skillProvider()
	if provider == nil {
		return nil, errSkillsUnavailable
	}
	if cursor != "" {
		return nil, errSkillCursor
	}
	name, path, ok := parseSkillURI(uri)
	if !ok || !g.CurrentSkillPolicy().Allows(name) {
		return nil, notFound(uri, "No directory is served at "+uri)
	}
	entries, err := provider.ReadSkillDirectory(ctx, name, path)
	if errors.Is(err, ErrSkillNotFound) {
		return nil, notFound(uri, "No directory is served at "+uri)
	}
	if err != nil {
		return nil, err
	}
	result := &ResourcesDirectoryResult{Resources: entries}
	attachSkillCacheMeta(&result.StatelessResultFields)
	return result, nil
}

func (g *Gateway) readSkillResource(ctx context.Context, uri string) (relayResult, error) {
	provider := g.skillProvider()
	name, path, ok := parseSkillURI(uri)
	if provider == nil || !ok || path == "" || !g.CurrentSkillPolicy().Allows(name) {
		return relayResult{}, notFound(uri, "")
	}
	content, err := provider.ReadSkillFile(ctx, name, path)
	if errors.Is(err, ErrSkillNotFound) {
		return relayResult{}, notFound(uri, "")
	}
	if err != nil {
		return relayResult{}, err
	}
	// A pointer preserves the required text property for an empty text file.
	item := struct {
		URI      string  `json:"uri"`
		MimeType string  `json:"mimeType"`
		Text     *string `json:"text,omitempty"`
		Blob     *string `json:"blob,omitempty"`
	}{URI: uri, MimeType: content.MimeType}
	if content.Blob != nil {
		blob := base64.StdEncoding.EncodeToString(content.Blob)
		item.Blob = &blob
	} else {
		item.Text = &content.Text
	}
	raw, err := json.Marshal(struct {
		Contents []any `json:"contents"`
	}{Contents: []any{item}})
	if err != nil {
		return relayResult{}, err
	}
	var fields StatelessResultFields
	attachSkillCacheMeta(&fields)
	return relayResult{raw: raw, fields: fields}, nil
}
