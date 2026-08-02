package contexts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/project"

	"gopkg.in/yaml.v3"
)

// Fragments mode: the canonical context store optionally becomes a
// directory of markdown rule fragments (~/.gridctl/context/fragments/*.md),
// each with optional YAML frontmatter (description, paths globs). The mode
// is strictly opt-in — while the directory does not exist every ctx surface
// behaves exactly as the single-file store always has, and no read-only
// path ever creates it. Composition order is filename-lexicographic;
// numeric prefixes (00-, 10-) are the ordering control.

const (
	fragmentsDirName = "fragments"
	// migratedFragmentName is where the canonical AGENTS.md lands when
	// fragments mode activates. The 00- prefix sorts it first; after
	// migration it is an ordinary fragment with no special casing.
	migratedFragmentName = "00-default"
)

// Fragment sentinel errors.
var (
	ErrFragmentsInactive = errors.New("fragments mode is not active; create one with 'gridctl ctx add <name>'")
	ErrNoFragment        = errors.New("no such fragment")
	ErrFragmentExists    = errors.New("fragment already exists")
	ErrBadFragmentName   = errors.New("fragment name must be lowercase letters, digits, and hyphens")
)

var fragmentNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// FragmentExtraField is one frontmatter key this package does not model,
// preserved in document order (the pkg/skills agent parser precedent) so
// write-backs never strip client extensions.
type FragmentExtraField struct {
	Key   string
	Value *yaml.Node
}

// Fragment is one rule fragment: optional frontmatter plus a markdown body.
// Raw is the file exactly as stored and is the identity-render payload.
type Fragment struct {
	// Name is the file base without .md; FileName includes it.
	Name     string
	FileName string
	// Description and Paths are the only modeled frontmatter keys. Paths
	// are glob strings passed to clients as metadata; gridctl never
	// evaluates them (the Copilot applyTo transform is the one rewrite).
	Description string
	Paths       []string
	Extra       []FragmentExtraField
	Body        string
	Raw         []byte
}

// FragmentsDir returns the fragment store directory.
func (m *Manager) FragmentsDir() string {
	return filepath.Join(m.Dir(), fragmentsDirName)
}

// FragmentsActive reports whether fragments mode is on: the directory
// exists. Read-only callers must treat false as "single-file store" and
// never create the directory themselves.
func (m *Manager) FragmentsActive() bool {
	info, err := os.Stat(m.FragmentsDir())
	return err == nil && info.IsDir()
}

// ValidateFragmentName rejects names that cannot become safe filenames.
func ValidateFragmentName(name string) error {
	if !fragmentNamePattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrBadFragmentName, name)
	}
	return nil
}

// ParseFragment parses one fragment file. Frontmatter is optional; the
// mapping is hand-walked so unknown keys survive in order and a duplicate
// key cannot discard the valid ones.
func ParseFragment(name string, data []byte) (*Fragment, error) {
	f := &Fragment{
		Name:     name,
		FileName: name + ".md",
		Raw:      append([]byte(nil), data...),
	}
	content := strings.ReplaceAll(string(data), "\r\n", "\n")

	fm, body, ok := splitFragmentFrontmatter(content)
	if !ok {
		f.Body = content
		return f, nil
	}
	f.Body = body

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &doc); err != nil {
		return nil, fmt.Errorf("fragment %s: parsing frontmatter: %w", f.FileName, err)
	}
	mapping := yamlFragmentMapping(&doc)
	if mapping == nil {
		return f, nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		keyNode, valNode := mapping.Content[i], mapping.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		switch keyNode.Value {
		case "description":
			if err := valNode.Decode(&f.Description); err != nil {
				return nil, fmt.Errorf("fragment %s: frontmatter key %q: %w", f.FileName, keyNode.Value, err)
			}
		case "paths":
			paths, err := decodeFragmentPaths(valNode)
			if err != nil {
				return nil, fmt.Errorf("fragment %s: frontmatter key %q: %w", f.FileName, keyNode.Value, err)
			}
			f.Paths = paths
		default:
			f.Extra = append(f.Extra, FragmentExtraField{Key: keyNode.Value, Value: valNode})
		}
	}
	return f, nil
}

// decodeFragmentPaths accepts a sequence of scalars or a single scalar
// (comma-separated), the tolerant shape clients themselves accept.
func decodeFragmentPaths(n *yaml.Node) ([]string, error) {
	switch n.Kind {
	case yaml.ScalarNode:
		var s string
		if err := n.Decode(&s); err != nil {
			return nil, err
		}
		var out []string
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(part); p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	case yaml.SequenceNode:
		var out []string
		if err := n.Decode(&out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("must be a list of glob strings or a comma-separated string")
}

// splitFragmentFrontmatter mirrors the agent-definition splitter: leading
// --- fences, body after the closing fence with one newline trimmed.
func splitFragmentFrontmatter(content string) (frontmatter, body string, ok bool) {
	trimmed := strings.TrimLeft(content, " \t")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", false
	}
	lines := strings.SplitAfter(content, "\n")
	openIdx, closeIdx := -1, -1
	for i, line := range lines {
		if strings.TrimSpace(strings.TrimRight(line, "\n")) == "---" {
			if openIdx == -1 {
				openIdx = i
			} else {
				closeIdx = i
				break
			}
		}
	}
	if closeIdx == -1 {
		return "", "", false
	}
	var fm, b strings.Builder
	for i := openIdx + 1; i < closeIdx; i++ {
		fm.WriteString(lines[i])
	}
	for i := closeIdx + 1; i < len(lines); i++ {
		b.WriteString(lines[i])
	}
	return fm.String(), strings.TrimPrefix(b.String(), "\n"), true
}

// yamlFragmentMapping unwraps a parsed document to its top-level mapping.
func yamlFragmentMapping(doc *yaml.Node) *yaml.Node {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	return doc
}

// RenderFragmentMD serializes a fragment back to its file form
// deterministically: modeled keys first in fixed order, then extras in
// their original document order, then the body. Used by write-backs
// (adopt); imported fragments keep their Raw bytes verbatim.
func RenderFragmentMD(f *Fragment) ([]byte, error) {
	hasFrontmatter := f.Description != "" || len(f.Paths) > 0 || len(f.Extra) > 0
	var b strings.Builder
	if hasFrontmatter {
		b.WriteString("---\n")
		fm := &yaml.Node{Kind: yaml.MappingNode}
		appendKV := func(key string, val *yaml.Node) {
			fm.Content = append(fm.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: key}, val)
		}
		if f.Description != "" {
			appendKV("description", &yaml.Node{Kind: yaml.ScalarNode, Value: f.Description})
		}
		if len(f.Paths) > 0 {
			seq := &yaml.Node{Kind: yaml.SequenceNode}
			for _, p := range f.Paths {
				seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: p})
			}
			appendKV("paths", seq)
		}
		for _, extra := range f.Extra {
			appendKV(extra.Key, extra.Value)
		}
		out, err := yaml.Marshal(fm)
		if err != nil {
			return nil, fmt.Errorf("fragment %s: rendering frontmatter: %w", f.FileName, err)
		}
		b.Write(out)
		b.WriteString("---\n")
	}
	if f.Body != "" {
		if hasFrontmatter {
			b.WriteString("\n")
		}
		b.WriteString(strings.TrimRight(f.Body, "\n"))
		b.WriteString("\n")
	}
	return []byte(b.String()), nil
}

// ListFragments returns every fragment in filename-lexicographic order —
// the composition order. Dotfiles (origin sidecars) and non-.md files are
// not fragments. Returns ErrFragmentsInactive when the mode is off so
// callers cannot accidentally treat "no directory" as "no fragments".
func (m *Manager) ListFragments() ([]*Fragment, error) {
	if !m.FragmentsActive() {
		return nil, ErrFragmentsInactive
	}
	entries, err := os.ReadDir(m.FragmentsDir())
	if err != nil {
		return nil, fmt.Errorf("reading fragments directory: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".md") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	fragments := make([]*Fragment, 0, len(names))
	for _, fileName := range names {
		data, err := os.ReadFile(filepath.Join(m.FragmentsDir(), fileName))
		if err != nil {
			return nil, fmt.Errorf("reading fragment %s: %w", fileName, err)
		}
		f, err := ParseFragment(strings.TrimSuffix(fileName, ".md"), data)
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, f)
	}
	return fragments, nil
}

// ReadFragment returns one fragment by name.
func (m *Manager) ReadFragment(name string) (*Fragment, error) {
	if !m.FragmentsActive() {
		return nil, ErrFragmentsInactive
	}
	data, err := os.ReadFile(m.fragmentPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNoFragment, name)
		}
		return nil, fmt.Errorf("reading fragment %s: %w", name, err)
	}
	return ParseFragment(name, data)
}

func (m *Manager) fragmentPath(name string) string {
	return filepath.Join(m.FragmentsDir(), name+".md")
}

// FragmentAddResult reports what AddFragment did, so the CLI can print the
// migration explicitly (the manager itself stays silent, matching
// InitFromClient).
type FragmentAddResult struct {
	// Migrated is true when this call activated fragments mode by moving
	// the canonical AGENTS.md to fragments/00-default.md.
	Migrated bool
	// MigratedBackup is the canonical file's backup path when Migrated.
	MigratedBackup string
	// CreatedPath is the new fragment's path.
	CreatedPath string
}

// AddFragment creates a fragment, activating fragments mode on first use:
// the existing canonical AGENTS.md (when present) is backed up and becomes
// fragments/00-default.md — an explicit, never-automatic migration.
func (m *Manager) AddFragment(name, content string) (FragmentAddResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res FragmentAddResult
	if err := ValidateFragmentName(name); err != nil {
		return res, err
	}

	if !m.FragmentsActive() {
		migrated, backup, err := m.activateFragments()
		if err != nil {
			return res, err
		}
		res.Migrated = migrated
		res.MigratedBackup = backup
	}

	path := m.fragmentPath(name)
	if _, err := os.Stat(path); err == nil {
		return res, fmt.Errorf("%w: %s", ErrFragmentExists, name)
	}
	if content == "" {
		content = "# " + name + "\n"
	}
	if err := atomicWriteFile(path, []byte(strings.TrimRight(content, "\n")+"\n")); err != nil {
		return res, err
	}
	res.CreatedPath = path
	return res, nil
}

// EnsureFragmentsActive activates fragments mode without creating a
// fragment (the pack import path: fragments arrive from a repo, but the
// migration of an existing canonical file must still happen first).
func (m *Manager) EnsureFragmentsActive() (FragmentAddResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var res FragmentAddResult
	if m.FragmentsActive() {
		return res, nil
	}
	migrated, backup, err := m.activateFragments()
	if err != nil {
		return res, err
	}
	res.Migrated = migrated
	res.MigratedBackup = backup
	return res, nil
}

// activateFragments creates the fragments directory, migrating an existing
// canonical file into it. Caller must hold m.mu.
func (m *Manager) activateFragments() (migrated bool, backup string, err error) {
	if err := os.MkdirAll(m.FragmentsDir(), 0755); err != nil {
		return false, "", fmt.Errorf("creating fragments directory: %w", err)
	}
	content, cerr := m.CanonicalContent()
	if cerr != nil {
		if errors.Is(cerr, ErrNoCanonical) {
			return false, "", nil
		}
		return false, "", cerr
	}
	backup, err = createBackup(m.CanonicalPath())
	if err != nil {
		return false, "", err
	}
	dest := m.fragmentPath(migratedFragmentName)
	if err := atomicWriteFile(dest, []byte(strings.TrimRight(content, "\n")+"\n")); err != nil {
		return false, "", err
	}
	if err := os.Remove(m.CanonicalPath()); err != nil {
		return false, "", fmt.Errorf("removing migrated canonical file: %w", err)
	}
	return true, backup, nil
}

// RemoveFragment deletes a fragment, backing it up first (out of tree, so
// nothing lingers in a directory that is now load-bearing configuration).
func (m *Manager) RemoveFragment(name string) (backupPath string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.FragmentsActive() {
		return "", ErrFragmentsInactive
	}
	path := m.fragmentPath(name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrNoFragment, name)
		}
		return "", err
	}
	backupPath, err = backupFragmentFile(m.home, "canonical", name, path)
	if err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("removing fragment %s: %w", name, err)
	}
	return backupPath, nil
}

// SaveFragment writes a fragment back to the store (the adopt path),
// backing up any existing file out of tree.
func (m *Manager) SaveFragment(f *Fragment) error {
	if err := ValidateFragmentName(f.Name); err != nil {
		return err
	}
	if !m.FragmentsActive() {
		return ErrFragmentsInactive
	}
	data, err := RenderFragmentMD(f)
	if err != nil {
		return err
	}
	path := m.fragmentPath(f.Name)
	if _, err := os.Stat(path); err == nil {
		if _, berr := backupFragmentFile(m.home, "canonical", f.Name, path); berr != nil {
			return berr
		}
	}
	return atomicWriteFile(path, data)
}

// InstallFragmentBytes writes raw fragment file content into the store
// (pack import path). Activates fragments mode if needed and reports the
// activation so callers can surface a migration explicitly — installing
// must never migrate the user's AGENTS.md silently. Existing files are
// backed up out of tree before overwrite.
func (m *Manager) InstallFragmentBytes(name string, data []byte) (FragmentAddResult, error) {
	var res FragmentAddResult
	if err := ValidateFragmentName(name); err != nil {
		return res, err
	}
	if _, err := ParseFragment(name, data); err != nil {
		return res, err
	}
	if !m.FragmentsActive() {
		activated, err := m.EnsureFragmentsActive()
		if err != nil {
			return res, err
		}
		res = activated
	}
	path := m.fragmentPath(name)
	if _, err := os.Stat(path); err == nil {
		if _, berr := backupFragmentFile(m.home, "canonical", name, path); berr != nil {
			return res, berr
		}
	}
	if err := atomicWriteFile(path, data); err != nil {
		return res, err
	}
	res.CreatedPath = path
	return res, nil
}

// UnsyncPackFragments removes every projected fragment file tagged with
// packName and returns the fragment names that lost their last projection
// of that pack (so the caller can decide whether to drop the store file).
func (m *Manager) UnsyncPackFragments(ctx context.Context, packName string) ([]UnsyncResult, []string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	var results []UnsyncResult
	var names []string
	seen := map[string]bool{}
	err := m.store.Mutate(ctx, false, func(pl *project.Lock) error {
		flf := fragmentViewFromLock(pl)
		dirty := false
		for _, name := range sortedFragmentNames(flf) {
			byClient := flf.Projections[name]
			for slug, entry := range byClient {
				if entry.Pack != packName {
					continue
				}
				if _, err := os.Stat(entry.Target); err == nil {
					if _, berr := backupFragmentFile(m.home, slug, name, entry.Target); berr != nil {
						return berr
					}
					if err := os.Remove(entry.Target); err != nil {
						return fmt.Errorf("removing %s: %w", entry.Target, err)
					}
					results = append(results, UnsyncResult{Slug: slug, TargetPath: entry.Target, Fragment: name, Action: "removed-file"})
				} else {
					results = append(results, UnsyncResult{Slug: slug, TargetPath: entry.Target, Fragment: name, Action: "already-gone"})
				}
				flf.remove(name, slug)
				dirty = true
				if !seen[name] {
					seen[name] = true
					names = append(names, name)
				}
			}
		}
		if dirty {
			return saveFragmentView(pl, flf)
		}
		return nil
	})
	return results, names, err
}

// composeResult is one compiled document plus the attribution the honesty
// surfaces need: which fragment hashes went in, and which fragments carry
// paths metadata a single-file target cannot express.
type composeResult struct {
	document    string
	inputHashes map[string]string
	// droppedPaths lists fragments whose paths globs do not survive
	// compilation (single-file targets have no per-file scoping).
	droppedPaths []string
}

// composeFragments concatenates fragment bodies in lexicographic order,
// each preceded by a human-readable Source marker. The markers are
// provenance for people reading the generated file, never machine state
// (Claude Code strips HTML comments from context); drift detection stays
// hash-based.
func composeFragments(fragments []*Fragment) composeResult {
	res := composeResult{inputHashes: make(map[string]string, len(fragments))}
	var b strings.Builder
	for _, f := range fragments {
		res.inputHashes[f.Name] = canonicalContentHash(string(f.Raw))
		if len(f.Paths) > 0 {
			res.droppedPaths = append(res.droppedPaths, f.Name)
		}
		body := strings.TrimSpace(f.Body)
		if body == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "<!-- Source: %s/%s -->\n\n", fragmentsDirName, f.FileName)
		b.WriteString(body)
		b.WriteString("\n")
	}
	res.document = b.String()
	return res
}
