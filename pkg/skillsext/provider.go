// Package skillsext exposes registry content through the Skills over MCP extension.
package skillsext

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillpins"
	"gopkg.in/yaml.v3"
)

const (
	MaxManifestFiles = 512
	MaxManifestBytes = 16_777_216
)

// ManifestSummary is the operator-facing projection of the full manifest.
type ManifestSummary struct {
	Digest  string `json:"digest"`
	Files   int    `json:"files"`
	Bytes   int64  `json:"bytes"`
	Dynamic bool   `json:"dynamic"`
}

// Summarize preserves the composite pin fingerprint even for dynamic skills.
func Summarize(manifest *skillpins.Manifest) ManifestSummary {
	result := ManifestSummary{Files: len(manifest.Files)}
	var files []skillpins.FileDigest
	for _, f := range manifest.Files {
		result.Bytes += f.Size
		if f.Path != "SKILL.md" {
			files = append(files, skillpins.FileDigest{Path: f.Path, Digest: "s1:" + f.Digest})
		}
	}
	if len(manifest.Files) > 0 {
		result.Digest = skillpins.CompositeHash("s1:"+manifest.Files[0].Digest, files)
	}
	result.Dynamic = result.Files > MaxManifestFiles || result.Bytes > MaxManifestBytes
	return result
}

type cachedSkill struct {
	entry       mcp.SkillEntry
	skillMD     []byte
	files       map[string]struct{}
	description string
}

// Provider caches complete manifests for one registry generation.
type Provider struct {
	store      *registry.Store
	mu         sync.Mutex
	generation uint64
	cache      map[string]*cachedSkill
}

func New(store *registry.Store) *Provider {
	return &Provider{store: store, cache: make(map[string]*cachedSkill)}
}

func (p *Provider) load(ctx context.Context, name string) (*cachedSkill, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		generation := p.store.Generation()
		if p.generation != generation {
			p.cache = make(map[string]*cachedSkill)
			p.generation = generation
		}
		if cached := p.cache[name]; cached != nil {
			return cached, nil
		}
		sk, err := p.store.GetSkill(name)
		if errors.Is(err, registry.ErrNotFound) {
			return nil, mcp.ErrSkillNotFound
		}
		if err != nil {
			return nil, err
		}
		if sk.State != registry.StateActive {
			return nil, mcp.ErrSkillNotFound
		}
		manifest, err := skillpins.ComputeManifest(ctx, p.store, sk)
		if err != nil {
			return nil, err
		}
		// A concurrent registry refresh invalidates both the skill and file walk.
		if p.store.Generation() != generation {
			continue
		}
		frontmatter, err := frontmatterJSON(manifest.SkillMD)
		if err != nil {
			return nil, fmt.Errorf("skill %q frontmatter: %w", name, err)
		}
		summary := Summarize(manifest)
		cached := &cachedSkill{
			entry:   mcp.SkillEntry{Name: name, URI: mcp.SkillURI(name, "SKILL.md"), Frontmatter: frontmatter, Dynamic: summary.Dynamic},
			skillMD: manifest.SkillMD, files: make(map[string]struct{}, len(manifest.Files)), description: sk.Description,
		}
		for _, f := range manifest.Files {
			cached.files[f.Path] = struct{}{}
			cached.entry.Resources = append(cached.entry.Resources, mcp.SkillResource{URI: mcp.SkillURI(name, f.Path), Digest: "sha256:" + f.Digest, Size: f.Size})
		}
		// URI escaping can change lexicographic order relative to disk paths.
		sort.Slice(cached.entry.Resources[1:], func(i, j int) bool {
			return cached.entry.Resources[i+1].URI < cached.entry.Resources[j+1].URI
		})
		p.cache[name] = cached
		if summary.Dynamic {
			slog.Warn("skill exceeds Skills over MCP manifest limits", "skill", name, "files", summary.Files, "bytes", summary.Bytes)
		}
		return cached, nil
	}
}

func frontmatterJSON(data []byte) (json.RawMessage, error) {
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, errors.New("invalid canonical frontmatter")
	}
	end := bytes.Index(data[4:], []byte("\n---\n"))
	if end < 0 {
		return nil, errors.New("invalid canonical frontmatter")
	}
	var fields map[string]any
	if err := yaml.Unmarshal(data[4:4+end], &fields); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

func cloneEntry(entry mcp.SkillEntry) mcp.SkillEntry {
	entry.Frontmatter = append(json.RawMessage(nil), entry.Frontmatter...)
	entry.Resources = append([]mcp.SkillResource(nil), entry.Resources...)
	return entry
}

func (p *Provider) ListSkillEntries(ctx context.Context) ([]mcp.SkillEntry, error) {
	entries := make([]mcp.SkillEntry, 0)
	for _, sk := range p.store.ActiveSkills() {
		entry, err := p.GetSkillEntry(ctx, sk.Name)
		if errors.Is(err, mcp.ErrSkillNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (p *Provider) GetSkillEntry(ctx context.Context, name string) (*mcp.SkillEntry, error) {
	cached, err := p.load(ctx, name)
	if err != nil {
		return nil, err
	}
	entry := cloneEntry(cached.entry)
	return &entry, nil
}

func (p *Provider) ReadSkillFile(ctx context.Context, name, file string) (*mcp.SkillFileContent, error) {
	cached, err := p.load(ctx, name)
	if err != nil {
		return nil, err
	}
	if _, ok := cached.files[file]; !ok {
		return nil, mcp.ErrSkillNotFound
	}
	data := cached.skillMD
	if file != "SKILL.md" {
		data, err = p.store.ReadManifestFile(ctx, name, file)
		if errors.Is(err, registry.ErrNotFound) {
			return nil, mcp.ErrSkillNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("read skill file: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := &mcp.SkillFileContent{MimeType: registry.ContentTypeForPath(file)}
	if utf8.Valid(data) && !bytes.ContainsRune(data, '\x00') {
		result.Text = string(data)
	} else {
		result.Blob = append([]byte(nil), data...)
	}
	return result, nil
}

func (p *Provider) ReadSkillDirectory(ctx context.Context, name, dir string) ([]mcp.SkillDirectoryEntry, error) {
	cached, err := p.load(ctx, name)
	if err != nil {
		return nil, err
	}
	if _, file := cached.files[dir]; file {
		return nil, mcp.ErrSkillNotFound
	}
	prefix := ""
	if dir != "" {
		prefix = dir + "/"
	}
	children := make(map[string]mcp.SkillDirectoryEntry)
	for file := range cached.files {
		if !strings.HasPrefix(file, prefix) {
			continue
		}
		rest := strings.TrimPrefix(file, prefix)
		child, _, directory := strings.Cut(rest, "/")
		childPath := path.Join(dir, child)
		entry := mcp.SkillDirectoryEntry{URI: mcp.SkillURI(name, childPath), Name: child, MimeType: registry.ContentTypeForPath(child)}
		if directory {
			entry.MimeType = "inode/directory"
		}
		if childPath == "SKILL.md" {
			entry.Name = name
			entry.Description = cached.description
		}
		children[child] = entry
	}
	if len(children) == 0 {
		return nil, mcp.ErrSkillNotFound
	}
	entries := make([]mcp.SkillDirectoryEntry, 0, len(children))
	for _, child := range children {
		entries = append(entries, child)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

var _ mcp.SkillProvider = (*Provider)(nil)
