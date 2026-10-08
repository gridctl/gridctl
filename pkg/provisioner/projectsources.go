package provisioner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gridctl/gridctl/pkg/git"
)

// ProjectSource is one project-scoped MCP file gridctl knows how to read.
// RelPath is relative to a project directory. ContainerKey is the object
// that holds server entries. Clients are the provisioner slugs that read
// the file. Scope is the label attached to candidates from the file.
type ProjectSource struct {
	RelPath      string
	ContainerKey string
	Clients      []string
	Scope        string
}

// ProjectSourceFile is one existing project file discovered by a walk.
// Dir is the project directory the relative path was resolved against.
// Depth 0 is the start directory. Path is the absolute file path.
type ProjectSourceFile struct {
	Dir          string
	Path         string
	Scope        string
	Clients      []string
	ContainerKey string
	Depth        int
}

// ImportRead is the status vocabulary for one selected config file.
// The name is neutral; the fields are the OpenCode read result.
type ImportRead = OpenCodeImportRead

// LocalScopeEntries is one Claude Code local-scope match, or a file-level
// status when the config could not be read. Dir is empty on a file-level
// status. A readable file with no matching project key yields no element.
type LocalScopeEntries struct {
	Dir     string
	Entries []ServerEntry
	Status  string
	Detail  string
}

// ProjectSources returns the project-scoped MCP files in scan order.
// opencode.jsonc precedes opencode.json so its entries win under
// first-occurrence keep. That order is a gridctl choice.
func ProjectSources() []ProjectSource {
	return []ProjectSource{
		{RelPath: ".mcp.json", ContainerKey: "mcpServers", Clients: []string{"claude-code", "vscode"}, Scope: "project"},
		{RelPath: filepath.Join(".cursor", "mcp.json"), ContainerKey: "mcpServers", Clients: []string{"cursor"}, Scope: "project"},
		{RelPath: filepath.Join(".vscode", "mcp.json"), ContainerKey: "servers", Clients: []string{"vscode"}, Scope: "project"},
		{RelPath: filepath.Join(".roo", "mcp.json"), ContainerKey: "mcpServers", Clients: []string{"roo"}, Scope: "project"},
		{RelPath: filepath.Join(".gemini", "settings.json"), ContainerKey: "mcpServers", Clients: []string{"gemini"}, Scope: "project"},
		{RelPath: filepath.Join(".zed", "settings.json"), ContainerKey: "context_servers", Clients: []string{"zed"}, Scope: "project"},
		{RelPath: "opencode.jsonc", ContainerKey: "mcp", Clients: []string{"opencode"}, Scope: "project"},
		{RelPath: "opencode.json", ContainerKey: "mcp", Clients: []string{"opencode"}, Scope: "project"},
	}
}

// ProjectWalk returns the directories project discovery visits, nearest
// first. The walk stops at the first git root, inclusive. When no directory
// in the chain is a git root, only startDir is returned. It does not read
// file contents and does not use the Gridctl home.
func ProjectWalk(ctx context.Context, startDir string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	startDir, err := absClean(startDir)
	if err != nil {
		return nil, err
	}
	var chain []string
	dir := startDir
	foundRoot := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chain = append(chain, dir)
		isRoot, err := git.IsRepoRoot(dir)
		if err != nil {
			return nil, err
		}
		if isRoot {
			foundRoot = true
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if !foundRoot {
		return []string{startDir}, nil
	}
	return chain, nil
}

// DiscoverProjectSources stats project files from startDir up to the nearest
// git root, inclusive. An empty clients filter matches every table row.
// A non-empty filter keeps rows whose client set intersects it, and the
// returned Clients slice is that intersection in table order. Missing paths
// are omitted. Only regular files are returned. File contents are not read.
func DiscoverProjectSources(ctx context.Context, startDir string, clients []string) ([]ProjectSourceFile, error) {
	dirs, err := ProjectWalk(ctx, startDir)
	if err != nil {
		return nil, err
	}
	table := ProjectSources()
	var out []ProjectSourceFile
	for depth, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, row := range table {
			hit, slugs := intersectClients(row.Clients, clients)
			if !hit {
				continue
			}
			path := filepath.Join(dir, row.RelPath)
			info, err := os.Stat(path)
			if err != nil || info.IsDir() || !info.Mode().IsRegular() {
				continue
			}
			out = append(out, ProjectSourceFile{
				Dir:          dir,
				Path:         path,
				Scope:        row.Scope,
				Clients:      slugs,
				ContainerKey: row.ContainerKey,
				Depth:        depth,
			})
		}
	}
	return out, nil
}

// ReadProjectSource reads exactly src.Path under src.ContainerKey.
// Statuses match ReadOpenCodeImport and carry no file contents.
// Cancellation is returned; a missing, unreadable, or malformed file is a
// status, not an error.
func ReadProjectSource(ctx context.Context, src ProjectSourceFile) (ImportRead, error) {
	if err := ctx.Err(); err != nil {
		return ImportRead{}, err
	}
	info, err := os.Stat(src.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return ImportRead{Status: OpenCodeImportMissing, Detail: "selected file does not exist"}, nil
		}
		return ImportRead{Status: OpenCodeImportUnreadable, Detail: "selected file could not be read"}, nil
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return ImportRead{Status: OpenCodeImportUnreadable, Detail: "selected path is a directory, not a config file"}, nil
	}
	if err := ctx.Err(); err != nil {
		return ImportRead{}, err
	}
	data, err := readJSONConfig(src.Path)
	if err != nil {
		if isConfigParseError(err) {
			return ImportRead{Status: OpenCodeImportMalformed, Detail: "selected file is not valid JSON or JSONC"}, nil
		}
		return ImportRead{Status: OpenCodeImportUnreadable, Detail: "selected file could not be read"}, nil
	}
	if data == nil {
		return ImportRead{Status: OpenCodeImportEmpty, Detail: "selected file is empty"}, nil
	}
	entries := listMapEntries(getMap(data, src.ContainerKey))
	if len(entries) == 0 {
		return ImportRead{Status: OpenCodeImportEmpty, Detail: "selected file has no MCP server entries"}, nil
	}
	return ImportRead{Status: OpenCodeImportSelected, Entries: entries}, nil
}

// ClaudeCodeLocalScope reads configPath once and returns mcpServers entries
// under projects.<dir> for each directory in dirs that has that object.
// Path keys are compared after filepath.Clean, and case-insensitively on
// Windows. A missing, empty, malformed, or unreadable file yields one
// status element and no entries. It does not abort the caller.
func ClaudeCodeLocalScope(ctx context.Context, configPath string, dirs []string) ([]LocalScopeEntries, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []LocalScopeEntries{{Status: OpenCodeImportMissing, Detail: "selected file does not exist"}}, nil
		}
		return []LocalScopeEntries{{Status: OpenCodeImportUnreadable, Detail: "selected file could not be read"}}, nil
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return []LocalScopeEntries{{Status: OpenCodeImportUnreadable, Detail: "selected path is a directory, not a config file"}}, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readJSONConfig(configPath)
	if err != nil {
		if isConfigParseError(err) {
			return []LocalScopeEntries{{Status: OpenCodeImportMalformed, Detail: "selected file is not valid JSON or JSONC"}}, nil
		}
		return []LocalScopeEntries{{Status: OpenCodeImportUnreadable, Detail: "selected file could not be read"}}, nil
	}
	if data == nil {
		return []LocalScopeEntries{{Status: OpenCodeImportEmpty, Detail: "selected file is empty"}}, nil
	}
	projects := getMap(data, "projects")
	if len(projects) == 0 {
		return []LocalScopeEntries{{Status: OpenCodeImportEmpty, Detail: "selected file has no MCP server entries"}}, nil
	}
	var out []LocalScopeEntries
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cleaned := filepath.Clean(dir)
		for key, v := range projects {
			if !projectPathEqual(key, cleaned) {
				continue
			}
			obj, ok := v.(map[string]any)
			if !ok {
				continue
			}
			entries := listMapEntries(getMap(obj, "mcpServers"))
			if len(entries) == 0 {
				continue
			}
			out = append(out, LocalScopeEntries{
				Dir:     cleaned,
				Entries: entries,
				Status:  OpenCodeImportSelected,
				Detail:  cleaned,
			})
			break
		}
	}
	return out, nil
}

func absClean(path string) (string, error) {
	if path == "" {
		return "", errors.New("project directory is empty")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return abs, nil
}

func intersectClients(row, filter []string) (bool, []string) {
	if len(filter) == 0 {
		return true, append([]string(nil), row...)
	}
	var hit []string
	for _, slug := range row {
		for _, want := range filter {
			if slug == want {
				hit = append(hit, slug)
				break
			}
		}
	}
	return len(hit) > 0, hit
}

func projectPathEqual(key, dir string) bool {
	key = filepath.Clean(key)
	dir = filepath.Clean(dir)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(key, dir)
	}
	return key == dir
}

func isConfigParseError(err error) bool {
	var pe *os.PathError
	return err != nil && !errors.As(err, &pe)
}
