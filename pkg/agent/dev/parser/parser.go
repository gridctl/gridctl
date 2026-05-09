// Package parser extracts a flat node list from a typed-skill source
// file. It is the AST surface the agent IDE uses to render both the
// Slice 1 textual node list and the Slice 3 React Flow canvas. Code
// is canon: every node here corresponds to a real call site in the
// author's source on disk and click-to-`$EDITOR` is a one-line jump
// to that file:line.
//
// The parser is read-only — it never mutates source. It is also
// deliberately strict about what it recognises: only the five graph
// primitives (tool, llm, parallel, handoff, approval) appear as
// nodes. Function bodies, types, imports, prompt strings, and
// control flow are not nodes; if a future slice ever needs them, it
// gets a separate extractor.
package parser

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// NodeKind is the closed vocabulary of agent-graph primitive call
// sites we recognise. Strings are stable JSON keys; do not rename
// without updating the frontend.
type NodeKind string

const (
	KindTool     NodeKind = "tool"
	KindLLM      NodeKind = "llm"
	KindParallel NodeKind = "parallel"
	KindHandoff  NodeKind = "handoff"
	KindApproval NodeKind = "approval"
)

// Lang names the source flavor a node was extracted from. Drives
// per-flavor decisions in the IDE (e.g. "needs explicit `agent
// build` to refresh" for Go, "hot-reloads on save" for TS).
type Lang string

const (
	LangGo Lang = "go"
	LangTS Lang = "ts"
)

// Node is one call site recognised in a skill's source. The shape is
// identical across Go and TS so the IDE renders both flavors against
// the same component tree.
type Node struct {
	// ID is a stable identifier within a skill — `kind:index` so
	// React Flow keys are deterministic across re-parses.
	ID string `json:"id"`

	// Kind is the recognised primitive (tool / llm / parallel /
	// handoff / approval).
	Kind NodeKind `json:"kind"`

	// Label is the human-readable label rendered on the node. For
	// `tool("server__name", ...)` this is the literal string; for
	// `llm({model: "claude-..."})` it is the model name; for
	// orchestration primitives it is a short summary.
	Label string `json:"label"`

	// File is the path to the source file (absolute when produced by
	// ParseFile, relative to the skill root when produced by
	// ParseSkill). The frontend joins it with the IDE root when it
	// constructs the `editor://` link.
	File string `json:"file"`

	// Line is the 1-based source line of the call expression.
	Line int `json:"line"`

	// Col is the 1-based column of the call expression.
	Col int `json:"col"`

	// Detail is an optional second line of context the IDE renders
	// in the node detail pane (e.g. the full first-argument string
	// when truncated). Empty when no extra context is helpful.
	Detail string `json:"detail,omitempty"`
}

// Graph is the parsed shape of one skill source file. Edges are
// implicit: each node points to its successor in declaration order.
// The frontend computes the edge list by zipping pairs.
type Graph struct {
	// Skill is the unprefixed skill name supplied by the caller.
	Skill string `json:"skill"`

	// Lang is the source flavor.
	Lang Lang `json:"lang"`

	// File is the parsed source file's path.
	File string `json:"file"`

	// Nodes is the ordered list of recognised call sites.
	Nodes []Node `json:"nodes"`

	// ParseError is non-empty when the source could not be parsed
	// cleanly; the IDE surfaces this as a stale-graph banner so the
	// last-good view stays visible until the author fixes the
	// syntax.
	ParseError string `json:"parse_error,omitempty"`
}

// ErrUnsupportedLang is returned when ParseFile is given a path with
// neither a .go nor .ts extension.
var ErrUnsupportedLang = errors.New("parser: unsupported source extension")

// ParseFile reads and parses a single source file. The skill name is
// the unprefixed identifier the IDE uses to key the resulting graph.
func ParseFile(skillName, path string) (Graph, error) {
	if skillName == "" {
		return Graph{}, errors.New("parser: skill name is required")
	}
	if path == "" {
		return Graph{}, errors.New("parser: path is required")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return parseGoFile(skillName, path)
	case ".ts", ".tsx", ".mts":
		return parseTSFile(skillName, path)
	default:
		return Graph{}, fmt.Errorf("%w: %q", ErrUnsupportedLang, path)
	}
}

// parseGoFile uses the standard go/ast walker to collect call sites
// matching the recognised primitives. We never panic on a parse error
// — instead we return a Graph with ParseError set so the IDE can
// keep the last-good view alongside the inline error overlay.
func parseGoFile(skillName, path string) (Graph, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return Graph{}, fmt.Errorf("parser: read %s: %w", path, err)
	}
	g := Graph{Skill: skillName, Lang: LangGo, File: path}

	fset := token.NewFileSet()
	f, parseErr := parser.ParseFile(fset, path, src, parser.AllErrors)
	if parseErr != nil {
		// Surface the parse error but continue extracting from
		// whatever ast.File the parser was able to produce — best
		// effort beats a blank canvas while the author is mid-edit.
		g.ParseError = parseErr.Error()
		if f == nil {
			return g, nil
		}
	}

	idx := make(map[NodeKind]int)
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		kind, label, ok := classifyGoCall(call)
		if !ok {
			return true
		}
		pos := fset.Position(call.Pos())
		i := idx[kind]
		idx[kind]++
		g.Nodes = append(g.Nodes, Node{
			ID:    fmt.Sprintf("%s:%d", kind, i),
			Kind:  kind,
			Label: label,
			File:  path,
			Line:  pos.Line,
			Col:   pos.Column,
		})
		return true
	})
	return g, nil
}

// classifyGoCall recognises the agent-runtime primitives in their
// canonical Go shapes. We accept both the bare identifiers (tool,
// llm, ...) used inside the typed Skill SDK and the `agent.Tool` /
// `agent.LLM` selector form authors use from outside the package.
func classifyGoCall(call *ast.CallExpr) (NodeKind, string, bool) {
	name := exprName(call.Fun)
	if name == "" {
		return "", "", false
	}
	kind, ok := goNameToKind[strings.ToLower(name)]
	if !ok {
		return "", "", false
	}
	label := firstStringArg(call)
	if label == "" {
		label = string(kind)
	}
	return kind, label, true
}

// goNameToKind is the recognition map for Go call sites. Both the
// bare and `agent.X` forms hit the same kind so the IDE doesn't
// care how the author imports the package.
var goNameToKind = map[string]NodeKind{
	"tool":         KindTool,
	"agent.tool":   KindTool,
	"calltool":     KindTool,
	"llm":          KindLLM,
	"agent.llm":    KindLLM,
	"chatmodel":    KindLLM,
	"parallel":     KindParallel,
	"agent.parallel": KindParallel,
	"handoff":      KindHandoff,
	"agent.handoff": KindHandoff,
	"approval":     KindApproval,
	"agent.approval": KindApproval,
}

// exprName returns the dotted name of a call's Fun expression, e.g.
// "tool" for an Ident or "agent.Tool" for a SelectorExpr. Anything
// more complex (chained selectors, type assertions) returns "" — we
// don't recognise those as primitive call sites.
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name + "." + x.Sel.Name
		}
	}
	return ""
}

// firstStringArg returns the literal string value of the first
// argument when it is a basic string literal. Returns "" for
// non-string or non-literal arguments — the IDE then falls back to
// the kind as the label.
func firstStringArg(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

// ParseSkill parses every recognised typed-skill source under skillDir
// and returns a single combined Graph. When both `skill.go` and
// `skill.ts` are present the Go file wins (matches the registry's
// detectHandler precedence) so the IDE renders one canonical graph
// per skill.
func ParseSkill(skillName, skillDir string) (Graph, error) {
	for _, name := range []string{"skill.go", "skill.ts"} {
		full := filepath.Join(skillDir, name)
		if _, err := os.Stat(full); err == nil {
			g, err := ParseFile(skillName, full)
			if err != nil {
				return Graph{}, err
			}
			// Re-root the file path to the skill directory so the
			// frontend renders relative paths in the node list.
			rel, relErr := filepath.Rel(skillDir, full)
			if relErr == nil {
				g.File = rel
				for i := range g.Nodes {
					g.Nodes[i].File = rel
				}
			}
			return g, nil
		}
	}
	return Graph{Skill: skillName, ParseError: "no typed handler (skill.go / skill.ts) found"}, nil
}
