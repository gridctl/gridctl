package api

import (
	"fmt"
	"strings"
)

// unifiedDiff renders a unified-style line diff of old vs new with `context`
// unchanged lines of surrounding context per hunk. It is intentionally small
// (an LCS line walk, no external dependency) since the only consumer is the
// client-scope preview, which diffs a single stack.yaml round-trip. Returns an
// empty string when the two inputs are identical.
//
// Output lines are prefixed " " (context), "-" (removed), or "+" (added) so the
// UI can render a copyable monospace patch. Hunk headers use the standard
// "@@ -a,b +c,d @@" form.
func unifiedDiff(oldText, newText string, context int) string {
	if oldText == newText {
		return ""
	}
	if context < 0 {
		context = 0
	}
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)

	ops := diffOps(oldLines, newLines)
	if len(ops) == 0 {
		return ""
	}

	// Group ops into hunks separated by runs of >2*context equal lines, keeping
	// up to `context` equal lines on each side of a change.
	var b strings.Builder
	for _, h := range buildHunks(ops, context) {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount)
		for _, ln := range h.lines {
			b.WriteString(ln)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// splitLines splits text into lines, dropping a single trailing newline so a
// file that ends in "\n" does not yield a spurious empty final line.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

type diffOp struct {
	kind byte // ' ' equal, '-' removed, '+' added
	text string
}

// diffOps computes a line-level diff via the classic LCS dynamic-programming
// table, then walks it into a flat op list in source order.
func diffOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	// lcs[i][j] = length of the LCS of a[i:] and b[j:].
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}

type hunk struct {
	oldStart, oldCount int
	newStart, newCount int
	lines              []string
}

// buildHunks slices the op stream into hunks, trimming long equal runs down to
// `context` lines on each side so the patch stays focused on the changes.
func buildHunks(ops []diffOp, context int) []hunk {
	// Mark which ops are within `context` of a change; everything else is elided.
	keep := make([]bool, len(ops))
	for i, op := range ops {
		if op.kind == ' ' {
			continue
		}
		lo := i - context
		if lo < 0 {
			lo = 0
		}
		hi := i + context
		if hi >= len(ops) {
			hi = len(ops) - 1
		}
		for k := lo; k <= hi; k++ {
			keep[k] = true
		}
	}

	var hunks []hunk
	oldLine, newLine := 1, 1
	i := 0
	for i < len(ops) {
		if !keep[i] {
			if ops[i].kind != '+' {
				oldLine++
			}
			if ops[i].kind != '-' {
				newLine++
			}
			i++
			continue
		}
		h := hunk{oldStart: oldLine, newStart: newLine}
		for i < len(ops) && keep[i] {
			op := ops[i]
			h.lines = append(h.lines, string(op.kind)+op.text)
			switch op.kind {
			case ' ':
				h.oldCount++
				h.newCount++
				oldLine++
				newLine++
			case '-':
				h.oldCount++
				oldLine++
			case '+':
				h.newCount++
				newLine++
			}
			i++
		}
		hunks = append(hunks, h)
	}
	return hunks
}
