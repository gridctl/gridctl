package hooksync

import (
	"fmt"
	"math"
	"strings"

	"github.com/gridctl/gridctl/pkg/skills"
)

// antigravityDefaultTimeoutSec is the deadline for a handler with no
// timeout: 600 seconds, the default the Claude Code hooks reference
// documents for command hooks (https://code.claude.com/docs/en/hooks).
// Antigravity's own default is 30 seconds, so the key always names one.
const antigravityDefaultTimeoutSec = 600

// antigravityTools maps Claude Code tool names to the Antigravity tool
// names a matcher selects. Edit covers both file replacement tools. A
// matcher token outside this table is unexpressible for Antigravity.
var antigravityTools = map[string]string{
	"Bash":      "run_command",
	"Edit":      "replace_file_content|multi_replace_file_content",
	"Glob":      "find_by_name",
	"Grep":      "grep_search",
	"Read":      "view_file",
	"WebFetch":  "read_url_content",
	"WebSearch": "search_web",
	"Write":     "write_to_file",
}

// antigravityHook is the value of the gridctl-<name> key: one hook
// definition in the shared hooks file. Field order fixes the written key
// order: enabled, then PreToolUse, then PostToolUse.
type antigravityHook struct {
	Enabled     bool               `json:"enabled"`
	PreToolUse  []antigravityGroup `json:"PreToolUse,omitempty"`
	PostToolUse []antigravityGroup `json:"PostToolUse,omitempty"`
}

type antigravityGroup struct {
	Matcher string               `json:"matcher"`
	Hooks   []antigravityHandler `json:"hooks"`
}

// antigravityHandler is one command handler. Antigravity has no exec
// form, so command is a shell string of single-quoted words.
type antigravityHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int64  `json:"timeout"`
}

// renderAntigravity renders the value of the hook's key in the shared
// Antigravity hooks file. PreToolUse and PostToolUse groups whose matcher
// maps through antigravityTools are carried; every other group is
// unexpressible and named. A script path or argument that cannot be
// single-quoted is an error, so nothing is written for the hook. The
// output is a pure function of the definition, the name, and the script
// directory.
func renderAntigravity(spec *hookSpec, name, scriptDir string) (rendered, error) {
	if err := skills.ValidateHookName(name); err != nil {
		return rendered{}, err
	}
	out := rendered{client: "antigravity"}
	value := antigravityHook{Enabled: true}
	for _, g := range spec.Groups {
		var list *[]antigravityGroup
		switch g.Event {
		case "PreToolUse":
			list = &value.PreToolUse
		case "PostToolUse":
			list = &value.PostToolUse
		default:
			out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("event %q has no antigravity mapping", g.Event))
			out.DroppedGroups = appendUnique(out.DroppedGroups, g.Event)
			continue
		}
		tokens, all, ok := matcherTokens(g)
		if !ok {
			pattern := fmt.Sprint(g.Matcher)
			out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("matcher %q is not a plain tool alternation", pattern))
			out.DroppedGroups = appendUnique(out.DroppedGroups, fmt.Sprintf("matcher %q", pattern))
			continue
		}
		matcher := "*"
		if !all {
			var tools []string
			missing := ""
			for _, token := range tokens {
				mapped, known := antigravityTools[token]
				if !known {
					missing = token
					break
				}
				for _, tool := range strings.Split(mapped, "|") {
					tools = appendUnique(tools, tool)
				}
			}
			if missing != "" {
				out.Unexpressible = append(out.Unexpressible, fmt.Sprintf("matcher token %q has no antigravity tool", missing))
				out.DroppedGroups = appendUnique(out.DroppedGroups, fmt.Sprintf("matcher %q", missing))
				continue
			}
			matcher = strings.Join(tools, "|")
		}
		group := antigravityGroup{Matcher: matcher, Hooks: []antigravityHandler{}}
		for _, h := range g.Handlers {
			command, err := antigravityCommand(h.scriptPath(scriptDir), h.args())
			if err != nil {
				return rendered{}, err
			}
			group.Hooks = append(group.Hooks, antigravityHandler{
				Type:    "command",
				Command: command,
				Timeout: antigravityTimeoutSec(h),
			})
			if cond, ok := h.Raw["if"].(string); ok {
				out.DroppedIf = appendUnique(out.DroppedIf, cond)
			}
			for _, key := range h.cosmeticKeys() {
				out.DroppedKeys = appendUnique(out.DroppedKeys, key)
			}
		}
		*list = append(*list, group)
	}
	sortStrings(out.DroppedKeys)
	if len(value.PreToolUse) == 0 && len(value.PostToolUse) == 0 {
		if len(out.Unexpressible) == 0 {
			out.Unexpressible = []string{"hook has no matcher groups"}
		}
		return out, nil
	}
	out.Value = value
	return out, nil
}

// antigravityCommand joins the script and its arguments into a shell
// string, each word wrapped in single quotes.
func antigravityCommand(script string, args []string) (string, error) {
	words := make([]string, 0, 1+len(args))
	for _, word := range append([]string{script}, args...) {
		quoted, err := singleQuote(word)
		if err != nil {
			return "", err
		}
		words = append(words, quoted)
	}
	return strings.Join(words, " "), nil
}

// singleQuote wraps s in single quotes. A value that contains a single
// quote, a newline, or a NUL byte is refused rather than escaped, so the
// command never needs a shell escape sequence.
func singleQuote(s string) (string, error) {
	if strings.ContainsAny(s, "'\n\x00") {
		return "", fmt.Errorf("cannot single-quote %q for the antigravity command: a single quote, newline, or NUL byte is refused", s)
	}
	return "'" + s + "'", nil
}

// antigravityTimeoutSec converts the handler's timeout to whole seconds,
// rounding up so a fractional deadline is never shortened, or returns the
// default deadline when it has none.
func antigravityTimeoutSec(h specHandler) int64 {
	sec, ok := h.timeoutSeconds()
	if !ok {
		return antigravityDefaultTimeoutSec
	}
	whole := math.Ceil(sec)
	if whole > math.MaxInt32 {
		whole = math.MaxInt32
	}
	return int64(whole)
}
