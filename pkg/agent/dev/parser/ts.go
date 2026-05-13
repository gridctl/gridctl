package parser

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// parseTSFile is a deliberately small lexer-style scan for the five
// recognised primitives. We do NOT load a TypeScript compiler — the
// IDE's startup budget is <3s and a real TS parser blows that. The
// supported call patterns are conservative on purpose:
//
//   tool(...)       llm(...)       parallel(...)
//   handoff(...)    approval(...)  agent.tool(...) etc.
//
// Plus a leading `await` is tolerated. Anything more elaborate
// (member chains, dynamic-property indexing) is not recognised, and
// the author still has the source-of-truth in `$EDITOR` to reach
// for. If a future slice ever needs full TS-AST awareness it should
// shell out to esbuild's metafile rather than embed a TS compiler.
func parseTSFile(skillName, path string) (Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return Graph{}, fmt.Errorf("parser: read %s: %w", path, err)
	}
	defer f.Close()

	g := Graph{Skill: skillName, Lang: LangTS, File: path, Nodes: []Node{}}
	idx := make(map[NodeKind]int)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	inBlockComment := false
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		stripped, blockState := stripTSComments(line, inBlockComment)
		inBlockComment = blockState
		if stripped == "" {
			continue
		}
		for _, hit := range scanTSLine(stripped) {
			i := idx[hit.kind]
			idx[hit.kind]++
			g.Nodes = append(g.Nodes, Node{
				ID:    fmt.Sprintf("%s:%d", hit.kind, i),
				Kind:  hit.kind,
				Label: hit.label,
				File:  path,
				Line:  lineNo,
				Col:   hit.col + 1,
			})
		}
	}
	if err := sc.Err(); err != nil {
		g.ParseError = err.Error()
	}
	return g, nil
}

// tsHit is a single recognised call within one source line.
type tsHit struct {
	kind  NodeKind
	label string
	col   int // 0-based column of the call
}

// tsKeywords maps the recognised primitive names (lowercase) to the
// node kind they emit. We accept both bare and `agent.X` forms. The
// matcher is greedy enough for skill source but not so wide that
// random code becomes nodes — every match must be followed by `(`.
var tsKeywords = map[string]NodeKind{
	"tool":     KindTool,
	"llm":      KindLLM,
	"parallel": KindParallel,
	"handoff":  KindHandoff,
	"approval": KindApproval,
}

// scanTSLine walks a single line scanning for `keyword(` patterns.
// It tolerates leading `await `, `agent.`, and `this.` so the
// recognised shapes mirror what TS skill authors actually write.
func scanTSLine(line string) []tsHit {
	var hits []tsHit
	i := 0
	for i < len(line) {
		// Find the next identifier-start character.
		if !isIdentStart(line[i]) {
			i++
			continue
		}
		start := i
		for i < len(line) && isIdentPart(line[i]) {
			i++
		}
		ident := line[start:i]
		if i >= len(line) || line[i] != '(' {
			continue
		}
		// `kind` may live on the bare ident or as the rightmost
		// segment of `agent.` / `this.` qualifiers — strip those.
		bare := ident
		if dot := strings.LastIndex(bare, "."); dot >= 0 {
			bare = bare[dot+1:]
		}
		kind, ok := tsKeywords[strings.ToLower(bare)]
		if !ok {
			continue
		}
		// Reject when preceded by `function ` or `=` followed by
		// `(` (arrow-function param list false positives). Cheap
		// best-effort: if the previous non-space token is `function`,
		// skip; if the ident is at column zero and immediately
		// followed by `(`, also skip when it looks like a function
		// declaration.
		if isDeclarationContext(line, start) {
			continue
		}
		col := start
		// Pick a label out of the parenthesised arguments when the
		// first character is a string literal.
		label := tsFirstStringArg(line[i:])
		if label == "" {
			label = string(kind)
		}
		hits = append(hits, tsHit{kind: kind, label: label, col: col})
	}
	return hits
}

// isIdentStart reports whether b can start a JS/TS identifier.
func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_' || b == '$'
}

// isIdentPart reports whether b can continue a JS/TS identifier.
func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9') || b == '.'
}

// isDeclarationContext returns true when the identifier at position
// start looks like the name half of a function declaration
// (`function tool(` etc.). Cheap heuristic — we look back past
// whitespace for the literal `function` token.
func isDeclarationContext(line string, start int) bool {
	j := start - 1
	for j >= 0 && (line[j] == ' ' || line[j] == '\t') {
		j--
	}
	if j < 7 { // need 8 chars: positions [j-7, j+1)
		return false
	}
	return strings.EqualFold(line[j-7:j+1], "function")
}

// tsFirstStringArg returns the literal value of the first string
// argument inside a parenthesised arg list. Returns "" when the
// first argument is not a quoted string literal — both single-,
// double-, and back-tick quotes are accepted.
func tsFirstStringArg(remaining string) string {
	if len(remaining) == 0 || remaining[0] != '(' {
		return ""
	}
	// Skip leading whitespace inside the parens.
	i := 1
	for i < len(remaining) && (remaining[i] == ' ' || remaining[i] == '\t') {
		i++
	}
	if i >= len(remaining) {
		return ""
	}
	q := remaining[i]
	if q != '"' && q != '\'' && q != '`' {
		return ""
	}
	i++
	var sb strings.Builder
	for i < len(remaining) {
		c := remaining[i]
		if c == '\\' && i+1 < len(remaining) {
			sb.WriteByte(remaining[i+1])
			i += 2
			continue
		}
		if c == q {
			return sb.String()
		}
		sb.WriteByte(c)
		i++
	}
	return ""
}

// stripTSComments removes line comments and tracks block-comment
// state across lines. The result still preserves source columns so
// hit reporting stays accurate; we only blank out the comment runs
// rather than collapse them.
func stripTSComments(line string, inBlock bool) (string, bool) {
	var b strings.Builder
	b.Grow(len(line))
	i := 0
	for i < len(line) {
		if inBlock {
			if i+1 < len(line) && line[i] == '*' && line[i+1] == '/' {
				inBlock = false
				b.WriteByte(' ')
				b.WriteByte(' ')
				i += 2
				continue
			}
			b.WriteByte(' ')
			i++
			continue
		}
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '/' {
			for i < len(line) {
				b.WriteByte(' ')
				i++
			}
			continue
		}
		if i+1 < len(line) && line[i] == '/' && line[i+1] == '*' {
			inBlock = true
			b.WriteByte(' ')
			b.WriteByte(' ')
			i += 2
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	return strings.TrimRight(b.String(), " \t"), inBlock
}
