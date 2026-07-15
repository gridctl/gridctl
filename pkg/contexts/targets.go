package contexts

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Strategy is how gridctl projects the canonical global context into one
// client's global context mechanism. Strategies are ordered by safety:
// a dedicated file gridctl fully owns, a single import line in a
// user-owned file, and a marker-delimited block in a shared file.
type Strategy string

const (
	// StrategyDedicatedFile writes a whole file gridctl owns inside a
	// rules directory the client reads (zero merge risk).
	StrategyDedicatedFile Strategy = "dedicated-file"
	// StrategyImportShim inserts one @-import line referencing the
	// canonical file; everything else in the target stays untouched.
	StrategyImportShim Strategy = "import-shim"
	// StrategyBlock writes the full file when the target is absent, or a
	// marker-delimited managed block when user content exists.
	StrategyBlock Strategy = "block"
)

// Target describes one client's global context surface. Paths are
// ~-templates expanded against the Manager's home directory, keyed by
// GOOS ("darwin", "linux", "windows").
type Target struct {
	Slug     string
	Name     string
	Strategy Strategy
	// Paths is the write target per OS. A missing key means the client
	// has no known global context path on that platform.
	Paths map[string]string
	// ImportPaths is where a user's pre-existing hand-written global
	// context most likely lives, for `ctx init --import`. Defaults to
	// Paths when empty.
	ImportPaths map[string]string
	// DetectDirs mark the client as initialized on this machine when any
	// of them exists. Sync refuses to create client config trees
	// wholesale, so these gate every write.
	DetectDirs []string
	// Frontmatter is prepended verbatim to dedicated files for clients
	// whose rules format requires it (e.g. VS Code *.instructions.md).
	Frontmatter string
	// MaxChars caps the rendered target file size; 0 means unlimited.
	// Windsurf enforces a 6,000-character limit on global_rules.md.
	MaxChars int
	// Experimental marks targets whose documented path rests on
	// unofficial sourcing; surfaced in status output.
	Experimental bool
}

// UnsupportedClient is a linked client with no writable global context
// file mechanism. Surfaced honestly in status output instead of hacks.
type UnsupportedClient struct {
	Slug   string
	Name   string
	Reason string
}

// allOS keys the same ~-template under every supported GOOS.
func allOS(path string) map[string]string {
	return map[string]string{"darwin": path, "linux": path, "windows": path}
}

// Targets returns the supported client targets in display order. The
// slugs match pkg/provisioner registry slugs so status output and the
// clients: block speak one identifier language.
func Targets() []Target {
	return []Target{
		{
			Slug:        "claude-code",
			Name:        "Claude Code",
			Strategy:    StrategyDedicatedFile,
			Paths:       allOS("~/.claude/rules/gridctl.md"),
			ImportPaths: allOS("~/.claude/CLAUDE.md"),
			DetectDirs:  []string{"~/.claude"},
		},
		{
			Slug:       "gemini",
			Name:       "Gemini CLI",
			Strategy:   StrategyImportShim,
			Paths:      allOS("~/.gemini/GEMINI.md"),
			DetectDirs: []string{"~/.gemini"},
		},
		{
			Slug:       "goose",
			Name:       "Goose",
			Strategy:   StrategyImportShim,
			Paths:      allOS("~/.config/goose/.goosehints"),
			DetectDirs: []string{"~/.config/goose"},
		},
		{
			Slug:       "opencode",
			Name:       "OpenCode",
			Strategy:   StrategyBlock,
			Paths:      allOS("~/.config/opencode/AGENTS.md"),
			DetectDirs: []string{"~/.config/opencode"},
		},
		{
			Slug:     "zed",
			Name:     "Zed",
			Strategy: StrategyBlock,
			Paths: map[string]string{
				"darwin":  "~/.config/zed/AGENTS.md",
				"linux":   "~/.config/zed/AGENTS.md",
				"windows": "~/AppData/Roaming/Zed/AGENTS.md",
			},
			DetectDirs: []string{"~/.config/zed", "~/AppData/Roaming/Zed"},
		},
		{
			Slug:       "cline",
			Name:       "Cline",
			Strategy:   StrategyBlock,
			Paths:      allOS("~/.agents/AGENTS.md"),
			DetectDirs: []string{"~/Documents/Cline", "~/.agents"},
		},
		{
			Slug:     "antigravity",
			Name:     "Antigravity",
			Strategy: StrategyBlock,
			Paths:    allOS("~/.gemini/AGENTS.md"),
			// Antigravity-specific dirs, not plain ~/.gemini, so a
			// Gemini-CLI-only install doesn't read as Antigravity.
			DetectDirs:   []string{"~/.gemini/config", "~/.gemini/antigravity"},
			Experimental: true,
		},
		{
			Slug:       "windsurf",
			Name:       "Windsurf",
			Strategy:   StrategyBlock,
			Paths:      allOS("~/.codeium/windsurf/memories/global_rules.md"),
			DetectDirs: []string{"~/.codeium/windsurf"},
			MaxChars:   6000,
		},
		{
			Slug:       "roo",
			Name:       "Roo Code",
			Strategy:   StrategyDedicatedFile,
			Paths:      allOS("~/.roo/rules/gridctl.md"),
			DetectDirs: []string{"~/.roo"},
		},
		{
			Slug:       "continue",
			Name:       "Continue",
			Strategy:   StrategyDedicatedFile,
			Paths:      allOS("~/.continue/rules/gridctl.md"),
			DetectDirs: []string{"~/.continue"},
		},
		{
			Slug:        "vscode",
			Name:        "VS Code (Copilot)",
			Strategy:    StrategyDedicatedFile,
			Paths:       allOS("~/.copilot/instructions/gridctl.instructions.md"),
			DetectDirs:  []string{"~/.copilot"},
			Frontmatter: "---\napplyTo: \"**\"\n---\n",
		},
	}
}

// Unsupported returns linked clients with no file mechanism to sync to.
func Unsupported() []UnsupportedClient {
	return []UnsupportedClient{
		{Slug: "claude", Name: "Claude Desktop", Reason: "instructions live in the app UI; no global context file"},
		{Slug: "cursor", Name: "Cursor", Reason: "global User Rules are stored in app-internal storage; no supported file path"},
		{Slug: "anythingllm", Name: "AnythingLLM", Reason: "workspace system prompt is UI/API only; no context file"},
		{Slug: "grok", Name: "Grok Build", Reason: "no documented global instruction file"},
	}
}

// FindTarget returns the supported target for slug.
func FindTarget(slug string) (Target, bool) {
	for _, t := range Targets() {
		if t.Slug == slug {
			return t, true
		}
	}
	return Target{}, false
}

// findUnsupported returns the unsupported entry for slug.
func findUnsupported(slug string) (UnsupportedClient, bool) {
	for _, u := range Unsupported() {
		if u.Slug == slug {
			return u, true
		}
	}
	return UnsupportedClient{}, false
}

// expandHome resolves a ~-template against home. Windows environment
// variables are expanded only on Windows, mirroring pkg/provisioner.
func expandHome(home, template string) string {
	if runtime.GOOS == "windows" {
		template = os.ExpandEnv(template)
	}
	if strings.HasPrefix(template, "~") {
		return filepath.Join(home, strings.TrimPrefix(template, "~"))
	}
	return template
}

// pathForOS returns the expanded path for the current OS, or "" when the
// client has no known global context path on this platform.
func pathForOS(home string, paths map[string]string) string {
	p, ok := paths[runtime.GOOS]
	if !ok || p == "" {
		return ""
	}
	return expandHome(home, p)
}

// targetPath resolves t's write path for this OS against home.
func (t Target) targetPath(home string) string {
	return pathForOS(home, t.Paths)
}

// importPath resolves the path `ctx init --import` reads from, falling
// back to the write path when no distinct import location exists.
func (t Target) importPath(home string) string {
	if len(t.ImportPaths) > 0 {
		if p := pathForOS(home, t.ImportPaths); p != "" {
			return p
		}
	}
	return t.targetPath(home)
}

// available reports whether any detect dir exists, i.e. the client is
// initialized on this machine.
func (t Target) available(home string) bool {
	for _, d := range t.DetectDirs {
		if info, err := os.Stat(expandHome(home, d)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}
