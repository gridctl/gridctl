package mcp

import (
	"context"
	"encoding/json"
	"errors"
)

const SkillsExtensionID = "io.modelcontextprotocol/skills"

// SkillResource binds a served file to its bytes.
type SkillResource struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// SkillEntry is a registry skill with a complete or dynamic manifest.
type SkillEntry struct {
	Name        string          `json:"-"`
	URI         string          `json:"uri"`
	Frontmatter json.RawMessage `json:"frontmatter"`
	Resources   []SkillResource `json:"-"`
	Dynamic     bool            `json:"-"`
}

func (s SkillEntry) MarshalJSON() ([]byte, error) {
	var resources any = s.Resources
	if s.Dynamic {
		resources = "dynamic"
	} else if s.Resources == nil {
		resources = []SkillResource{}
	}
	return json.Marshal(struct {
		URI         string          `json:"uri"`
		Frontmatter json.RawMessage `json:"frontmatter"`
		Resources   any             `json:"resources"`
	}{s.URI, s.Frontmatter, resources})
}

type SkillFileContent struct {
	MimeType string
	Text     string
	Blob     []byte
}

type SkillDirectoryEntry struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType"`
}

var ErrSkillNotFound = errors.New("skill not found")

// SkillProvider owns registry content; the gateway applies exposure policy.
// ListSkillEntries receives the caller's name filter so a denied skill is not
// hashed. A nil filter includes every active skill. The gateway still drops
// denied names from the returned slice.
type SkillProvider interface {
	ListSkillEntries(context.Context, func(string) bool) ([]SkillEntry, error)
	GetSkillEntry(context.Context, string) (*SkillEntry, error)
	ReadSkillFile(context.Context, string, string) (*SkillFileContent, error)
	ReadSkillDirectory(context.Context, string, string) ([]SkillDirectoryEntry, error)
}
