package provisioner

import (
	"context"
	"errors"
	"os"
	"strings"
)

// OpenCode import file statuses. They describe the selected file only.
// A failed read does not fall through to another file.
const (
	OpenCodeImportSelected   = "selected"
	OpenCodeImportMissing    = "missing"
	OpenCodeImportUnreadable = "unreadable"
	OpenCodeImportMalformed  = "malformed"
	OpenCodeImportEmpty      = "empty"
)

// OpenCodeImportPolicy explains why import reads one file instead of
// OpenCode's merged effective configuration.
const OpenCodeImportPolicy = "This is a compatibility-preserving single-file choice, not OpenCode's merge or writable-file preference. OpenCode merges config.json, then opencode.json, then opencode.jsonc, and prefers opencode.jsonc for writes. config.json is not read automatically."

// OpenCodeImportSelection is the single config file chosen for an OpenCode
// import. Detect's write target is a separate contract and is not changed here.
type OpenCodeImportSelection struct {
	Path        string
	JSONPath    string
	JSONCPath   string
	Explicit    bool
	BothPresent bool
	JSONAbsent  bool
	Notes       []string
}

// OpenCodeImportRead is the result of reading exactly one selected file.
type OpenCodeImportRead struct {
	Status  string
	Detail  string
	Entries []ServerEntry
}

// DiscoverOpenCodeImport chooses the OpenCode file import will read.
// An empty explicit path uses the Gridctl home (GRIDCTL_HOME, otherwise the
// OS home): existing opencode.json, or opencode.jsonc only when that JSON
// file is absent. It does not scan the working directory, XDG_CONFIG_HOME,
// OPENCODE_CONFIG, OPENCODE_CONFIG_DIR, OPENCODE_CONFIG_CONTENT, or config.json.
// A non-empty explicit path is returned unchanged, with no sibling lookup.
func DiscoverOpenCodeImport(ctx context.Context, explicit string) (OpenCodeImportSelection, error) {
	if err := ctx.Err(); err != nil {
		return OpenCodeImportSelection{}, err
	}
	if explicit != "" {
		return OpenCodeImportSelection{
			Path:     explicit,
			Explicit: true,
			Notes: []string{
				"Reading the explicit --source-config path only. There is no fallback if this file is missing, unreadable, or malformed.",
				"config.json, project files, XDG_CONFIG_HOME, OPENCODE_CONFIG, OPENCODE_CONFIG_DIR, and OPENCODE_CONFIG_CONTENT are not consulted.",
			},
		}, nil
	}
	jsonPath := configPathForPlatform(newOpenCode().paths)
	if jsonPath == "" {
		return OpenCodeImportSelection{}, errNoOpenCodePath
	}
	jsoncPath := jsoncSibling(jsonPath)
	if err := ctx.Err(); err != nil {
		return OpenCodeImportSelection{}, err
	}
	jsonExists := fileExists(jsonPath)
	jsoncExists := fileExists(jsoncPath)
	sel := OpenCodeImportSelection{JSONPath: jsonPath, JSONCPath: jsoncPath}
	switch {
	case jsonExists && jsoncExists:
		sel.Path = jsonPath
		sel.BothPresent = true
		sel.Notes = []string{
			"Selected " + jsonPath + ". Sibling " + jsoncPath + " was not merged.",
			OpenCodeImportPolicy,
		}
	case jsonExists:
		sel.Path = jsonPath
		sel.Notes = []string{
			"Selected " + jsonPath + ". No sibling opencode.jsonc was found.",
			OpenCodeImportPolicy,
			"Pass --source-config to read a file outside the Gridctl home. Project files are discovered separately.",
		}
	case jsoncExists:
		sel.Path = jsoncPath
		sel.JSONAbsent = true
		sel.Notes = []string{
			"Selected " + jsoncPath + " because " + jsonPath + " is absent.",
			OpenCodeImportPolicy,
		}
	default:
		sel.Notes = []string{
			"No opencode.json or opencode.jsonc exists under the Gridctl home (" + jsonPath + ", " + jsoncPath + ").",
			OpenCodeImportPolicy,
			"XDG_CONFIG_HOME is not scanned for this user-scope file. Pass --source-config to select a file outside the Gridctl home.",
		}
	}
	return sel, nil
}

// ReadOpenCodeImport reads exactly path. It does not search for a sibling
// or a fallback file. Cancellation is checked before the read; the status
// is value-free and does not include file contents.
func ReadOpenCodeImport(ctx context.Context, path string) (OpenCodeImportRead, error) {
	if err := ctx.Err(); err != nil {
		return OpenCodeImportRead{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return OpenCodeImportRead{Status: OpenCodeImportMissing, Detail: "selected file does not exist"}, nil
		}
		return OpenCodeImportRead{Status: OpenCodeImportUnreadable, Detail: "selected file could not be read"}, nil
	}
	if info.IsDir() {
		return OpenCodeImportRead{Status: OpenCodeImportUnreadable, Detail: "selected path is a directory, not a config file"}, nil
	}
	if err := ctx.Err(); err != nil {
		return OpenCodeImportRead{}, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- caller-selected import source, not a write target
	if err != nil {
		return OpenCodeImportRead{Status: OpenCodeImportUnreadable, Detail: "selected file could not be read"}, nil
	}
	raw = stripBOM(raw)
	if len(strings.TrimSpace(string(raw))) == 0 {
		return OpenCodeImportRead{Status: OpenCodeImportEmpty, Detail: "selected file is empty"}, nil
	}
	data, _, err := parseJSON(raw)
	if err != nil {
		return OpenCodeImportRead{Status: OpenCodeImportMalformed, Detail: "selected file is not valid JSON or JSONC"}, nil
	}
	entries := listMapEntries(getMap(data, "mcp"))
	if len(entries) == 0 {
		return OpenCodeImportRead{Status: OpenCodeImportEmpty, Detail: "selected file has no MCP server entries"}, nil
	}
	return OpenCodeImportRead{Status: OpenCodeImportSelected, Entries: entries}, nil
}

func jsoncSibling(jsonPath string) string {
	if strings.HasSuffix(jsonPath, ".json") {
		return strings.TrimSuffix(jsonPath, ".json") + ".jsonc"
	}
	return jsonPath + ".jsonc"
}

var errNoOpenCodePath = errors.New("OpenCode import has no config path for this operating system")
