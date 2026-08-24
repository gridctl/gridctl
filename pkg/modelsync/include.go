package modelsync

import (
	"fmt"
	"regexp"
	"strings"
)

// Include mutation modes recorded in Entry.IncludeMode so unsync can
// restore the prior shape.
const (
	includeCreated  = "created"  // no include: key existed; gridctl added it
	includeAppended = "appended" // appended one item to an existing list
	includePromoted = "promoted" // promoted a scalar include to a list
	includeFlow     = "flow"     // inserted into a flow-style [a, b] list
)

// The parent LiteLLM config is edited as text lines on purpose: a
// parse-and-re-marshal round trip would strip comments and reorder
// keys, and the acceptance bar for the parent file is byte-identity
// outside the one managed line. All matching happens on CRLF-normalized
// content; callers restore CRLF on write.

var (
	includeKeyRe  = regexp.MustCompile(`^include:[ \t]*(.*)$`)
	includeItemRe = regexp.MustCompile(`^([ \t]+)-[ \t]+(.+?)[ \t]*$`)
)

// includeEdit is the outcome of an upsert: the new content and what was
// done, for the lockfile record.
type includeEdit struct {
	Content  string
	Mode     string
	Original string
}

// normalizeIncludeItem strips a trailing YAML comment and matching
// quotes from an include entry, so `"gridctl-models.yaml"` and
// `gridctl-models.yaml  # managed` both compare equal to the bare ref
// instead of reading as a missing (and then duplicated) line.
func normalizeIncludeItem(s string) string {
	value, _ := splitInlineComment(s)
	return unquoteScalar(value)
}

// unquoteScalar removes one matching pair of single or double quotes.
func unquoteScalar(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// hasIncludeLine reports whether content's include entries contain ref.
func hasIncludeLine(content, ref string) bool {
	form, keyIdx, lines := findIncludeKey(normalizeNewlines(content))
	switch form {
	case "none":
		return false
	case "scalar":
		return normalizeIncludeItem(includeKeyRe.FindStringSubmatch(lines[keyIdx])[1]) == ref
	case "flow":
		return flowContains(lines[keyIdx], ref)
	}
	for _, idx := range includeItemIndexes(lines, keyIdx) {
		if m := includeItemRe.FindStringSubmatch(lines[idx]); m != nil && normalizeIncludeItem(m[2]) == ref {
			return true
		}
	}
	return false
}

// upsertIncludeLine adds ref to the parent config's include entries,
// touching nothing but the include key. Idempotent: an already-present
// ref returns the normalized content unchanged with mode "".
func upsertIncludeLine(content, ref string) (includeEdit, error) {
	norm := normalizeNewlines(content)
	if hasIncludeLine(norm, ref) {
		return includeEdit{Content: norm}, nil
	}
	form, keyIdx, lines := findIncludeKey(norm)
	switch form {
	case "none":
		out := norm
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += "include:\n  - " + ref + "\n"
		return includeEdit{Content: out, Mode: includeCreated}, nil

	case "bare":
		items := includeItemIndexes(lines, keyIdx)
		indent, insertAfter := "  ", keyIdx
		if len(items) > 0 {
			last := items[len(items)-1]
			if m := includeItemRe.FindStringSubmatch(lines[last]); m != nil {
				indent = m[1]
			}
			insertAfter = last
		}
		lines = insertLine(lines, insertAfter+1, indent+"- "+ref)
		return includeEdit{Content: strings.Join(lines, "\n"), Mode: includeAppended}, nil

	case "scalar":
		rest := includeKeyRe.FindStringSubmatch(lines[keyIdx])[1]
		value, comment := splitInlineComment(rest)
		keyLine := "include:"
		if comment != "" {
			keyLine += " " + comment
		}
		lines[keyIdx] = keyLine
		lines = insertLine(lines, keyIdx+1, "  - "+value)
		lines = insertLine(lines, keyIdx+2, "  - "+ref)
		return includeEdit{Content: strings.Join(lines, "\n"), Mode: includePromoted, Original: value}, nil

	case "flow":
		line, err := flowInsert(lines[keyIdx], ref)
		if err != nil {
			return includeEdit{}, err
		}
		lines[keyIdx] = line
		return includeEdit{Content: strings.Join(lines, "\n"), Mode: includeFlow}, nil
	}
	return includeEdit{}, fmt.Errorf("unrecognized include form %q", form)
}

// removeIncludeLine removes ref, undoing the recorded mutation mode: a
// created key is dropped entirely when gridctl's item was its only
// entry, and a promoted scalar is restored when only the original
// remains.
func removeIncludeLine(content, ref, mode, original string) (string, error) {
	norm := normalizeNewlines(content)
	if !hasIncludeLine(norm, ref) {
		return norm, nil
	}
	form, keyIdx, lines := findIncludeKey(norm)
	switch form {
	case "flow":
		lines[keyIdx] = flowRemove(lines[keyIdx], ref)
		return strings.Join(lines, "\n"), nil
	case "scalar":
		// A scalar include of exactly our ref (written by hand, or
		// adopted at sync time) is removed by dropping the key line: it
		// is the one-item form of a created list.
		if normalizeIncludeItem(includeKeyRe.FindStringSubmatch(lines[keyIdx])[1]) == ref {
			return strings.Join(removeLineAt(lines, keyIdx), "\n"), nil
		}
		return norm, nil
	case "bare":
		items := includeItemIndexes(lines, keyIdx)
		remaining := make([]string, 0, len(items))
		refIdx := -1
		for _, idx := range items {
			m := includeItemRe.FindStringSubmatch(lines[idx])
			if m != nil && normalizeIncludeItem(m[2]) == ref && refIdx == -1 {
				refIdx = idx
				continue
			}
			if m != nil {
				remaining = append(remaining, m[2])
			}
		}
		if refIdx == -1 {
			return norm, nil
		}
		lines = removeLineAt(lines, refIdx)
		switch {
		case mode == includeCreated && len(remaining) == 0:
			lines = removeLineAt(lines, keyIdx)
		case mode == includePromoted && len(remaining) == 1 && normalizeIncludeItem(remaining[0]) == unquoteScalar(original):
			// Restore the scalar form, keeping any comment that rode the
			// key line through the promotion. Only when the surviving item
			// still sits directly under the key; a rearranged list stays a
			// list rather than risking an unrelated line.
			if m := includeItemRe.FindStringSubmatch(lines[keyIdx+1]); m != nil && normalizeIncludeItem(m[2]) == unquoteScalar(original) {
				_, comment := splitInlineComment(strings.TrimPrefix(lines[keyIdx], "include:"))
				restored := "include: " + original
				if comment != "" {
					restored += " " + comment
				}
				lines[keyIdx] = restored
				lines = removeLineAt(lines, keyIdx+1)
			}
		}
		return strings.Join(lines, "\n"), nil
	}
	return norm, fmt.Errorf("include entry %q not removable from %s-form include", ref, form)
}

// findIncludeKey locates the top-level include: key. Forms: "none",
// "bare" (key alone, items on following lines), "scalar", "flow".
func findIncludeKey(norm string) (form string, keyIdx int, lines []string) {
	lines = strings.Split(norm, "\n")
	for i, line := range lines {
		m := includeKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rest := strings.TrimSpace(m[1])
		value, _ := splitInlineComment(rest)
		switch {
		case value == "":
			return "bare", i, lines
		case strings.HasPrefix(value, "["):
			return "flow", i, lines
		default:
			return "scalar", i, lines
		}
	}
	return "none", -1, lines
}

// includeItemIndexes returns the line indexes of the list items
// belonging to a bare include: key at keyIdx. The block ends at the
// first line that is neither an indented item, an indented comment,
// nor blank-before-more-items.
func includeItemIndexes(lines []string, keyIdx int) []int {
	var items []int
	for i := keyIdx + 1; i < len(lines); i++ {
		line := lines[i]
		if includeItemRe.MatchString(line) {
			items = append(items, i)
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || (strings.HasPrefix(trimmed, "#") && line != trimmed) {
			// A blank or indented comment only continues the block when
			// another item follows.
			if nextItemFollows(lines, i) {
				continue
			}
		}
		break
	}
	return items
}

func nextItemFollows(lines []string, from int) bool {
	for j := from + 1; j < len(lines); j++ {
		if includeItemRe.MatchString(lines[j]) {
			return true
		}
		trimmed := strings.TrimSpace(lines[j])
		if trimmed == "" || (strings.HasPrefix(trimmed, "#") && lines[j] != trimmed) {
			continue
		}
		return false
	}
	return false
}

// splitInlineComment separates a scalar value from a trailing YAML
// comment. A comment begins at "#" preceded by whitespace (or at the
// start), which a plain scalar cannot contain.
func splitInlineComment(s string) (value, comment string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		return "", s
	}
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
			return strings.TrimSpace(s[:i]), s[i:]
		}
	}
	return s, ""
}

// flowContains reports whether a flow-style include line lists ref.
func flowContains(line, ref string) bool {
	for _, item := range flowItems(line) {
		if item == ref {
			return true
		}
	}
	return false
}

func flowItems(line string) []string {
	open := strings.Index(line, "[")
	close := strings.LastIndex(line, "]")
	if open == -1 || close <= open {
		return nil
	}
	var items []string
	for _, part := range strings.Split(line[open+1:close], ",") {
		item := strings.Trim(strings.TrimSpace(part), `"'`)
		if item != "" {
			items = append(items, item)
		}
	}
	return items
}

func flowInsert(line, ref string) (string, error) {
	close := strings.LastIndex(line, "]")
	if close == -1 {
		return "", fmt.Errorf("multi-line flow include lists are not supported; use block style")
	}
	before := strings.TrimRight(line[:close], " \t")
	sep := ", "
	if strings.HasSuffix(before, "[") {
		sep = ""
	}
	return before + sep + ref + line[close:], nil
}

func flowRemove(line, ref string) string {
	open := strings.Index(line, "[")
	close := strings.LastIndex(line, "]")
	if open == -1 || close <= open {
		return line
	}
	var kept []string
	for _, item := range flowItems(line) {
		if item != ref {
			kept = append(kept, item)
		}
	}
	return line[:open+1] + strings.Join(kept, ", ") + line[close:]
}

func insertLine(lines []string, at int, line string) []string {
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:at]...)
	out = append(out, line)
	out = append(out, lines[at:]...)
	return out
}

func removeLineAt(lines []string, at int) []string {
	return append(lines[:at], lines[at+1:]...)
}
