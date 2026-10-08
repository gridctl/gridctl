package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/internal/importer"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// importCollectOpts is the scope selection for one import scan.
// Explicit is a resolved or raw --source-config path. When it is set, no
// other source is read.
type importCollectOpts struct {
	Scope      string
	ProjectDir string
	Explicit   string
}

// importSource is one config file in precedence order. Entries are already
// read. ListErr is a test seam for an unreadable in-memory client.
type importSource struct {
	Slugs    []string
	Path     string
	Scope    string
	OwnerDir string
	Entries  []provisioner.ServerEntry
	ListErr  error
	Doc      importSourceDoc
}

func collectSources(ctx context.Context, registry *provisioner.Registry, client string, opts importCollectOpts) ([]importSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := requireKnownClient(registry, client); err != nil {
		return nil, err
	}
	if opts.Explicit != "" {
		return collectExplicitSource(ctx, registry, client, opts.Explicit)
	}
	switch opts.Scope {
	case "user":
		if client != "" && !clientDetected(registry, client) {
			return nil, provisioner.ErrClientNotFound
		}
		return collectUserSources(ctx, registry, client, "")
	case "project":
		project, err := collectProjectSources(ctx, client, opts.ProjectDir)
		if err != nil {
			return nil, err
		}
		if client != "" && len(project) == 0 && !clientDetected(registry, client) {
			return nil, provisioner.ErrClientNotFound
		}
		return project, nil
	default:
		dirs, err := provisioner.ProjectWalk(ctx, opts.ProjectDir)
		if err != nil {
			return nil, err
		}
		local, err := collectLocalSources(ctx, registry, client, dirs)
		if err != nil {
			return nil, err
		}
		project, err := collectProjectSourcesIn(ctx, client, dirs)
		if err != nil {
			return nil, err
		}
		custom, err := collectCustomSource(ctx, client)
		if err != nil {
			return nil, err
		}
		if client != "" && !clientDetected(registry, client) && len(project) == 0 && len(custom) == 0 {
			return nil, provisioner.ErrClientNotFound
		}
		user, err := collectUserSources(ctx, registry, client, opts.ProjectDir)
		if err != nil {
			return nil, err
		}
		// A home directory that is a git root makes a user file match a
		// project-table path. List that file once, as the user row.
		project = omitProjectRowsDuplicatingUser(project, user)
		out := make([]importSource, 0, len(local)+len(project)+len(custom)+len(user))
		out = append(out, local...)
		out = append(out, project...)
		out = append(out, custom...)
		out = append(out, user...)
		return out, nil
	}
}

func requireKnownClient(registry *provisioner.Registry, client string) error {
	if client == "" {
		return nil
	}
	if _, ok := registry.FindBySlug(client); !ok {
		return unknownClientError(registry, client)
	}
	return nil
}

func clientDetected(registry *provisioner.Registry, client string) bool {
	prov, ok := registry.FindBySlug(client)
	if !ok {
		return false
	}
	_, found := prov.Detect()
	return found
}

func collectExplicitSource(ctx context.Context, registry *provisioner.Registry, client, rawPath string) ([]importSource, error) {
	prov, ok := registry.FindBySlug(client)
	if !ok {
		return nil, unknownClientError(registry, client)
	}
	path, err := resolveSourceConfig(rawPath)
	if err != nil {
		return nil, err
	}
	if prov.Slug() == "opencode" {
		sel, err := provisioner.DiscoverOpenCodeImport(ctx, path)
		if err != nil {
			return nil, err
		}
		read, err := provisioner.ReadOpenCodeImport(ctx, path)
		if err != nil {
			return nil, err
		}
		row := sourceFromRead([]string{"opencode"}, path, "user", "", read)
		row.Doc.Notes = append([]string(nil), sel.Notes...)
		return []importSource{row}, nil
	}
	row, err := readProvisionerSource(ctx, prov, path, "user")
	if err != nil {
		return nil, err
	}
	return []importSource{row}, nil
}

func collectProjectSources(ctx context.Context, client, projectDir string) ([]importSource, error) {
	var filter []string
	if client != "" {
		filter = []string{client}
	}
	found, err := provisioner.DiscoverProjectSources(ctx, projectDir, filter)
	if err != nil {
		return nil, err
	}
	return projectSourcesFromFiles(ctx, found)
}

func collectProjectSourcesIn(ctx context.Context, client string, dirs []string) ([]importSource, error) {
	var filter []string
	if client != "" {
		filter = []string{client}
	}
	found, err := provisioner.DiscoverProjectSourcesIn(ctx, dirs, filter)
	if err != nil {
		return nil, err
	}
	return projectSourcesFromFiles(ctx, found)
}

func projectSourcesFromFiles(ctx context.Context, found []provisioner.ProjectSourceFile) ([]importSource, error) {
	out := make([]importSource, 0, len(found))
	for _, file := range found {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		read, err := provisioner.ReadProjectSource(ctx, file)
		if err != nil {
			return nil, err
		}
		// OwnerDir is the project directory the relative path was resolved
		// against, not the nested config parent. ${workspaceFolder} means
		// that project, including for .cursor/mcp.json and .vscode/mcp.json.
		out = append(out, sourceFromRead(file.Clients, file.Path, file.Scope, file.Dir, read))
	}
	return out, nil
}

func collectLocalSources(ctx context.Context, registry *provisioner.Registry, client string, dirs []string) ([]importSource, error) {
	if client != "" && client != "claude-code" {
		return nil, nil
	}
	prov, ok := registry.FindBySlug("claude-code")
	if !ok {
		return nil, nil
	}
	configPath, found := prov.Detect()
	if !found || configPath == "" {
		return nil, nil
	}
	matches, err := provisioner.ClaudeCodeLocalScope(ctx, configPath, dirs)
	if err != nil {
		return nil, err
	}
	out := make([]importSource, 0, len(matches))
	for _, match := range matches {
		read := provisioner.ImportRead{Status: match.Status, Detail: match.Detail, Entries: match.Entries}
		row := sourceFromRead([]string{"claude-code"}, configPath, "local", match.Dir, read)
		out = append(out, row)
	}
	return out, nil
}

func collectCustomSource(ctx context.Context, client string) ([]importSource, error) {
	if client != "" && client != "opencode" {
		return nil, nil
	}
	path := os.Getenv("OPENCODE_CONFIG")
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		path = abs
	}
	read, err := provisioner.ReadOpenCodeImport(ctx, path)
	if err != nil {
		return nil, err
	}
	row := sourceFromRead([]string{"opencode"}, path, "custom", "", read)
	return []importSource{row}, nil
}

func collectUserSources(ctx context.Context, registry *provisioner.Registry, client, claudeOwner string) ([]importSource, error) {
	var out []importSource
	for _, slug := range registry.AllSlugs() {
		if client != "" && slug != client {
			continue
		}
		prov, ok := registry.FindBySlug(slug)
		if !ok {
			continue
		}
		if slug == "opencode" {
			row, err := collectOpenCodeUser(ctx, prov)
			if err != nil {
				return nil, err
			}
			if row != nil {
				out = append(out, *row)
			}
			continue
		}
		configPath, found := prov.Detect()
		if !found {
			continue
		}
		row, err := readProvisionerSource(ctx, prov, configPath, "user")
		if err != nil {
			return nil, err
		}
		if slug == "claude-code" && claudeOwner != "" {
			row.OwnerDir = claudeOwner
		}
		out = append(out, row)
	}
	return out, nil
}

func collectOpenCodeUser(ctx context.Context, prov provisioner.ClientProvisioner) (*importSource, error) {
	if _, found := prov.Detect(); !found {
		return nil, nil
	}
	sel, err := provisioner.DiscoverOpenCodeImport(ctx, "")
	if err != nil {
		return nil, err
	}
	doc := importSourceDoc{
		Client:  prov.Slug(),
		Clients: []string{prov.Slug()},
		Path:    sel.Path,
		Scope:   "user",
		Notes:   append([]string(nil), sel.Notes...),
	}
	if sel.BothPresent {
		doc.AlternatePath = sel.JSONCPath
	}
	if sel.Path == "" {
		doc.Status = provisioner.OpenCodeImportMissing
		doc.Detail = "neither opencode.json nor opencode.jsonc exists under the Gridctl home"
		if sel.JSONPath != "" {
			doc.Path = sel.JSONPath
		}
		row := importSource{Slugs: []string{prov.Slug()}, Path: doc.Path, Scope: "user", Doc: doc}
		return &row, nil
	}
	read, err := provisioner.ReadOpenCodeImport(ctx, sel.Path)
	if err != nil {
		return nil, err
	}
	row := sourceFromRead([]string{prov.Slug()}, sel.Path, "user", "", read)
	row.Doc.AlternatePath = doc.AlternatePath
	row.Doc.Notes = doc.Notes
	return &row, nil
}

func readProvisionerSource(ctx context.Context, prov provisioner.ClientProvisioner, path, scope string) (importSource, error) {
	slug := prov.Slug()
	row := importSource{
		Slugs: []string{slug},
		Path:  path,
		Scope: scope,
		Doc: importSourceDoc{
			Client:  slug,
			Clients: []string{slug},
			Path:    path,
			Scope:   scope,
		},
	}
	if err := ctx.Err(); err != nil {
		return importSource{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			row.Doc.Status = provisioner.OpenCodeImportMissing
			row.Doc.Detail = "selected file does not exist"
			return row, nil
		}
		row.Doc.Status = provisioner.OpenCodeImportUnreadable
		row.Doc.Detail = "selected file could not be read"
		return row, nil
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		row.Doc.Status = provisioner.OpenCodeImportUnreadable
		row.Doc.Detail = "selected path is a directory, not a config file"
		return row, nil
	}
	if err := ctx.Err(); err != nil {
		return importSource{}, err
	}
	entries, err := prov.ListServers(path)
	if err != nil {
		row.Doc.Status = provisioner.OpenCodeImportMalformed
		row.Doc.Detail = "selected file is not valid JSON or JSONC"
		if isReadError(err) {
			row.Doc.Status = provisioner.OpenCodeImportUnreadable
			row.Doc.Detail = "selected file could not be read"
		}
		return row, nil
	}
	if len(entries) == 0 {
		row.Doc.Status = provisioner.OpenCodeImportEmpty
		row.Doc.Detail = "selected file has no MCP server entries"
		return row, nil
	}
	row.Entries = entries
	row.Doc.Status = provisioner.OpenCodeImportSelected
	row.Doc.count = len(entries)
	return row, nil
}

func sourceFromRead(slugs []string, path, scope, owner string, read provisioner.ImportRead) importSource {
	copied := append([]string(nil), slugs...)
	doc := importSourceDoc{
		Path:   path,
		Scope:  scope,
		Status: read.Status,
		Detail: read.Detail,
		count:  len(read.Entries),
	}
	if len(copied) > 0 {
		doc.Client = copied[0]
		doc.Clients = append([]string(nil), copied...)
	}
	return importSource{
		Slugs:    copied,
		Path:     path,
		Scope:    scope,
		OwnerDir: owner,
		Entries:  read.Entries,
		Doc:      doc,
	}
}

func sourceDocs(sources []importSource) []importSourceDoc {
	if len(sources) == 0 {
		return nil
	}
	docs := make([]importSourceDoc, len(sources))
	for i, src := range sources {
		docs[i] = src.Doc
	}
	return docs
}

func originsFor(src importSource) []importer.Origin {
	out := make([]importer.Origin, 0, len(src.Slugs))
	for _, slug := range src.Slugs {
		out = append(out, importer.Origin{Client: slug, Scope: src.Scope, Path: src.Path})
	}
	return out
}

func withOrigins(c importer.Candidate, origins []importer.Origin) importer.Candidate {
	c.Origins = append([]importer.Origin(nil), origins...)
	seen := map[string]bool{}
	for _, o := range origins {
		if c.Source == "" {
			c.Source = o.Client
		}
		if o.Path != "" {
			appendSourcePath(&c, o.Path)
		}
		if o.Client != "" && !seen[o.Client] {
			seen[o.Client] = true
			c.FoundIn = append(c.FoundIn, o.Client)
		}
	}
	sort.Strings(c.FoundIn)
	return c
}

func mergeSkip(dst *importer.Candidate, src importer.Candidate) {
	for _, o := range src.Origins {
		if !originListed(dst.Origins, o) {
			dst.Origins = append(dst.Origins, o)
		}
	}
	for _, slug := range src.FoundIn {
		if !stringListed(dst.FoundIn, slug) {
			dst.FoundIn = append(dst.FoundIn, slug)
		}
	}
	sort.Strings(dst.FoundIn)
	if dst.Source == "" {
		dst.Source = src.Source
	}
	appendSourcePath(dst, src.SourcePath)
	for _, path := range src.SourcePaths {
		appendSourcePath(dst, path)
	}
}

func originListed(origins []importer.Origin, o importer.Origin) bool {
	for _, existing := range origins {
		if existing == o {
			return true
		}
	}
	return false
}

func stringListed(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func isReadError(err error) bool {
	var pe *os.PathError
	return errors.As(err, &pe)
}

// omitProjectRowsDuplicatingUser drops a project row, or the overlapping
// clients on a shared row, when that path is already a user row for the
// same client. The user row remains.
func omitProjectRowsDuplicatingUser(project, user []importSource) []importSource {
	if len(project) == 0 || len(user) == 0 {
		return project
	}
	out := make([]importSource, 0, len(project))
	for _, row := range project {
		var keep []string
		for _, slug := range row.Slugs {
			if userOwnsPath(user, slug, row.Path) {
				continue
			}
			keep = append(keep, slug)
		}
		if len(keep) == 0 {
			continue
		}
		if len(keep) != len(row.Slugs) {
			row.Slugs = keep
			row.Doc.Clients = append([]string(nil), keep...)
			row.Doc.Client = keep[0]
		}
		out = append(out, row)
	}
	return out
}

func userOwnsPath(user []importSource, slug, path string) bool {
	for _, row := range user {
		if row.Scope != "" && row.Scope != "user" {
			continue
		}
		if !importPathsEqual(row.Path, path) {
			continue
		}
		for _, existing := range row.Slugs {
			if existing == slug {
				return true
			}
		}
	}
	return false
}

func importPathsEqual(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func validateImportScope(scope string) error {
	switch scope {
	case "all", "user", "project":
		return nil
	default:
		return fmt.Errorf("--scope: %q is not valid (allowed: all, user, project)", scope)
	}
}

func resolveProjectDir(flag string) (string, error) {
	var path string
	if flag == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolving project directory: %w", err)
		}
		path = cwd
	} else if filepath.IsAbs(flag) {
		path = flag
	} else {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolving --project-dir: %w", err)
		}
		path = filepath.Join(cwd, flag)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		shown := flag
		if shown == "" {
			shown = path
		}
		return "", fmt.Errorf("--project-dir: %s is not a directory", shown)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving --project-dir: %w", err)
	}
	return abs, nil
}

func formatProvenance(c importer.Candidate) string {
	return c.ProvenanceLabel()
}

func formatSourceStatus(s importSourceDoc) string {
	status := s.Status
	if s.Status == provisioner.OpenCodeImportSelected && s.count > 0 {
		noun := "server"
		if s.count != 1 {
			noun = "servers"
		}
		status = fmt.Sprintf("selected (%d %s)", s.count, noun)
	}
	if s.Scope == "local" && s.Status == provisioner.OpenCodeImportSelected && s.Detail != "" {
		status += " for " + s.Detail
	}
	return status
}
