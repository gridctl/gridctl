package proposals

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FilePatch is one file section of a unified diff. Path is the cleaned
// path relative to the target root. New is true for an added file
// (--- /dev/null).
type FilePatch struct {
	Path  string
	New   bool
	Hunks []Hunk
}

// Hunk is one @@ section. Starts are 1-based line numbers as written in
// the header; a zero count means the hunk inserts after OldStart.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Lines              []HunkLine
}

// HunkLine is one body line. Op is ' ', '-', or '+'. NoNewline is set
// when the line was followed by "\ No newline at end of file".
type HunkLine struct {
	Op        byte
	Text      string
	NoNewline bool
}

// patchPaths returns the sorted paths a patch set touches.
func patchPaths(patches []FilePatch) []string {
	out := make([]string, 0, len(patches))
	for _, p := range patches {
		out = append(out, p.Path)
	}
	sort.Strings(out)
	return out
}

// ParseUnified parses a unified diff with one or more file sections. CRLF
// line endings are normalized to LF first. Git headers that describe a
// deletion, rename, copy, mode change, or binary patch are refused with
// ErrDiffUnsupported, as is a section whose new side is /dev/null or whose
// old and new paths differ. Each path is checked with cleanPatchPath.
// Text outside a file section other than "diff " and "index " lines is
// refused with ErrInvalidInput.
func ParseUnified(text string) ([]FilePatch, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("%w: diff is not UTF-8 text", ErrInvalidInput)
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var patches []FilePatch
	seen := map[string]bool{}
	i := 0
	for i < len(lines) {
		line := lines[i]
		switch {
		case strings.HasPrefix(line, "--- "):
			fp, next, err := parseFileSection(lines, i)
			if err != nil {
				return nil, err
			}
			if seen[fp.Path] {
				return nil, fmt.Errorf("%w: diff has more than one section for %s", ErrInvalidInput, fp.Path)
			}
			seen[fp.Path] = true
			patches = append(patches, fp)
			i = next
		case line == "", strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "new file mode "):
			i++
		case unsupportedGitHeader(line):
			return nil, fmt.Errorf("%w: line %d is a %q header; only modifications and added text files are supported", ErrDiffUnsupported, i+1, gitHeaderName(line))
		default:
			return nil, fmt.Errorf("%w: line %d is outside a file section", ErrInvalidInput, i+1)
		}
	}
	if len(patches) == 0 {
		return nil, fmt.Errorf("%w: diff has no file sections", ErrInvalidInput)
	}
	return patches, nil
}

var unsupportedHeaders = []string{
	"deleted file mode", "old mode", "new mode", "rename from", "rename to",
	"copy from", "copy to", "similarity index", "dissimilarity index",
	"Binary files", "GIT binary patch",
}

func unsupportedGitHeader(line string) bool {
	return gitHeaderName(line) != ""
}

func gitHeaderName(line string) string {
	for _, h := range unsupportedHeaders {
		if strings.HasPrefix(line, h) {
			return h
		}
	}
	return ""
}

// parseFileSection parses one "--- / +++" section starting at lines[i].
func parseFileSection(lines []string, i int) (FilePatch, int, error) {
	var fp FilePatch
	oldName := headerPath(strings.TrimPrefix(lines[i], "--- "))
	if i+1 >= len(lines) || !strings.HasPrefix(lines[i+1], "+++ ") {
		return fp, 0, fmt.Errorf("%w: line %d: '---' header is not followed by '+++'", ErrInvalidInput, i+1)
	}
	newName := headerPath(strings.TrimPrefix(lines[i+1], "+++ "))
	if newName == "/dev/null" {
		return fp, 0, fmt.Errorf("%w: deleting %s is not supported", ErrDiffUnsupported, displayPath(oldName))
	}
	var raw string
	if oldName == "/dev/null" {
		fp.New = true
		raw = strings.TrimPrefix(newName, "b/")
	} else {
		o, n := oldName, newName
		if strings.HasPrefix(o, "a/") && strings.HasPrefix(n, "b/") {
			o, n = o[2:], n[2:]
		}
		if o != n {
			return fp, 0, fmt.Errorf("%w: renaming %s to %s is not supported", ErrDiffUnsupported, displayPath(o), displayPath(n))
		}
		raw = n
	}
	clean, err := cleanPatchPath(raw)
	if err != nil {
		return fp, 0, err
	}
	fp.Path = clean
	i += 2
	for i < len(lines) && strings.HasPrefix(lines[i], "@@") {
		h, next, err := parseHunk(lines, i, fp.Path, len(fp.Hunks)+1)
		if err != nil {
			return fp, 0, err
		}
		fp.Hunks = append(fp.Hunks, h)
		i = next
	}
	if len(fp.Hunks) == 0 {
		return fp, 0, fmt.Errorf("%w: section for %s has no hunks", ErrInvalidInput, fp.Path)
	}
	if fp.New {
		h := fp.Hunks[0]
		if len(fp.Hunks) != 1 || h.OldStart != 0 || h.OldLines != 0 {
			return fp, 0, fmt.Errorf("%w: added file %s must have one hunk starting at -0,0", ErrInvalidInput, fp.Path)
		}
	}
	return fp, i, nil
}

// headerPath strips a trailing tab-separated timestamp from a file header.
func headerPath(s string) string {
	if idx := strings.IndexByte(s, '\t'); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimRight(s, " ")
}

// displayPath bounds a refused path for error text.
func displayPath(p string) string {
	if len(p) > 128 {
		p = p[:128] + "..."
	}
	return strconv.Quote(p)
}

func parseHunk(lines []string, i int, file string, index int) (Hunk, int, error) {
	var h Hunk
	if err := parseHunkHeader(lines[i], &h); err != nil {
		return h, 0, fmt.Errorf("%w: hunk %d of %s: %v", ErrInvalidInput, index, file, err)
	}
	i++
	oldSeen, newSeen := 0, 0
	for oldSeen < h.OldLines || newSeen < h.NewLines {
		if i >= len(lines) {
			return h, 0, fmt.Errorf("%w: hunk %d of %s ends before its stated line counts", ErrInvalidInput, index, file)
		}
		line := lines[i]
		var hl HunkLine
		switch {
		case line == "":
			hl = HunkLine{Op: ' '}
		case line[0] == ' ' || line[0] == '-' || line[0] == '+':
			hl = HunkLine{Op: line[0], Text: line[1:]}
		case line[0] == '\\':
			return h, 0, fmt.Errorf("%w: hunk %d of %s has a misplaced no-newline marker", ErrInvalidInput, index, file)
		default:
			return h, 0, fmt.Errorf("%w: hunk %d of %s ends before its stated line counts", ErrInvalidInput, index, file)
		}
		switch hl.Op {
		case ' ':
			oldSeen++
			newSeen++
		case '-':
			oldSeen++
		case '+':
			newSeen++
		}
		if oldSeen > h.OldLines || newSeen > h.NewLines {
			return h, 0, fmt.Errorf("%w: hunk %d of %s has more lines than its header states", ErrInvalidInput, index, file)
		}
		h.Lines = append(h.Lines, hl)
		i++
		if i < len(lines) && strings.HasPrefix(lines[i], `\`) {
			h.Lines[len(h.Lines)-1].NoNewline = true
			i++
		}
	}
	return h, i, nil
}

// parseHunkHeader reads "@@ -l[,s] +l[,s] @@[ section]".
func parseHunkHeader(line string, h *Hunk) error {
	rest := strings.TrimPrefix(line, "@@ ")
	end := strings.Index(rest, " @@")
	if !strings.HasPrefix(line, "@@ ") || end < 0 {
		return fmt.Errorf("malformed hunk header")
	}
	fields := strings.Fields(rest[:end])
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "-") || !strings.HasPrefix(fields[1], "+") {
		return fmt.Errorf("malformed hunk header")
	}
	var err error
	if h.OldStart, h.OldLines, err = parseRange(fields[0][1:]); err != nil {
		return err
	}
	if h.NewStart, h.NewLines, err = parseRange(fields[1][1:]); err != nil {
		return err
	}
	return nil
}

func parseRange(s string) (int, int, error) {
	start, count := s, "1"
	if idx := strings.IndexByte(s, ','); idx >= 0 {
		start, count = s[:idx], s[idx+1:]
	}
	a, err := strconv.Atoi(start)
	if err != nil || a < 0 {
		return 0, 0, fmt.Errorf("malformed hunk range")
	}
	b, err := strconv.Atoi(count)
	if err != nil || b < 0 {
		return 0, 0, fmt.Errorf("malformed hunk range")
	}
	if b > 0 && a == 0 {
		return 0, 0, fmt.Errorf("malformed hunk range")
	}
	return a, b, nil
}

// cleanPatchPath normalizes a diff path with path.Clean and refuses an
// empty, absolute, or escaping path, a path with a dot-prefixed component,
// a .tmp file, a backslash or NUL, and non-UTF-8 text. The refusals match
// the files skillpins never hashes, so every file a proposal writes is in
// the pinned set.
func cleanPatchPath(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: a file section has an empty path", ErrDiffUnsupported)
	}
	if !utf8.ValidString(raw) || strings.ContainsAny(raw, "\x00\\") {
		return "", fmt.Errorf("%w: path %s is not a plain relative path", ErrDiffUnsupported, displayPath(raw))
	}
	if strings.HasPrefix(raw, "/") || path.IsAbs(raw) {
		return "", fmt.Errorf("%w: path %s is absolute", ErrDiffUnsupported, displayPath(raw))
	}
	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: path %s leaves the target directory", ErrDiffUnsupported, displayPath(raw))
		}
	}
	clean := path.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: path %s does not name a file in the target directory", ErrDiffUnsupported, displayPath(raw))
	}
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("%w: path %s has a dot-prefixed component", ErrDiffUnsupported, displayPath(raw))
		}
	}
	if strings.HasSuffix(clean, ".tmp") {
		return "", fmt.Errorf("%w: path %s is a temporary file", ErrDiffUnsupported, displayPath(raw))
	}
	return clean, nil
}

// ApplyStrict applies one file patch to current content. current is nil
// for an added file. CRLF is normalized to LF before matching and the
// result is LF. Every context and removal line must match exactly at the
// hunk's stated offset; there is no fuzz and no offset search. A mismatch
// names the file and hunk index without quoting content.
func ApplyStrict(current []byte, p FilePatch) ([]byte, error) {
	if p.New && current != nil {
		return nil, fmt.Errorf("%w: %s already exists; the diff adds it as a new file", ErrApplyFailed, p.Path)
	}
	if !p.New && current == nil {
		return nil, fmt.Errorf("%w: %s does not exist; the diff modifies it", ErrApplyFailed, p.Path)
	}
	text := strings.ReplaceAll(string(current), "\r\n", "\n")
	var lines []string
	finalNewline := true
	if text != "" {
		lines = strings.Split(text, "\n")
		if lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		} else {
			finalNewline = false
		}
	}
	out := make([]string, 0, len(lines))
	cursor := 0
	for hi, h := range p.Hunks {
		idx := h.OldStart - 1
		if h.OldLines == 0 {
			idx = h.OldStart
		}
		if idx < cursor || idx > len(lines) {
			return nil, hunkMismatch(p.Path, hi+1)
		}
		out = append(out, lines[cursor:idx]...)
		pos := idx
		var added []HunkLine
		for _, hl := range h.Lines {
			if hl.Op == ' ' || hl.Op == '-' {
				if pos >= len(lines) || lines[pos] != hl.Text {
					return nil, hunkMismatch(p.Path, hi+1)
				}
				atEOFNoNewline := pos == len(lines)-1 && !finalNewline
				if hl.NoNewline != atEOFNoNewline {
					return nil, hunkMismatch(p.Path, hi+1)
				}
				pos++
			}
			if hl.Op == ' ' || hl.Op == '+' {
				added = append(added, hl)
			}
		}
		for _, hl := range added {
			out = append(out, hl.Text)
		}
		cursor = pos
		if cursor == len(lines) {
			if len(added) > 0 {
				finalNewline = !added[len(added)-1].NoNewline
			} else {
				finalNewline = true
			}
		} else {
			for _, hl := range added {
				if hl.NoNewline {
					return nil, hunkMismatch(p.Path, hi+1)
				}
			}
		}
	}
	out = append(out, lines[cursor:]...)
	if len(out) == 0 {
		return []byte{}, nil
	}
	result := strings.Join(out, "\n")
	if finalNewline {
		result += "\n"
	}
	return []byte(result), nil
}

func hunkMismatch(file string, index int) error {
	return fmt.Errorf("%w: hunk %d of %s does not match the current content", ErrApplyFailed, index, file)
}

// addedText returns the added lines of a patch joined by newlines. A
// stale proposal whose diff no longer applies is scanned over this text.
func addedText(p FilePatch) string {
	var b strings.Builder
	for _, h := range p.Hunks {
		for _, hl := range h.Lines {
			if hl.Op == '+' {
				b.WriteString(hl.Text)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}
