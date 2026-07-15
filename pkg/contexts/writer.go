package contexts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Managed-region markers and header. The header names the source of truth
// and the edit command instead of a bare DO NOT EDIT, so a reader knows
// where changes belong.
const (
	beginMarker = "<!-- BEGIN GRIDCTL MANAGED -->"
	endMarker   = "<!-- END GRIDCTL MANAGED -->"

	blockHeader = "<!-- Managed by gridctl. Edit with 'gridctl ctx edit' or the web UI; " +
		"this content is overwritten on sync. Content outside the gridctl block is yours. -->"
	fileHeader = "<!-- Managed by gridctl. Edit with 'gridctl ctx edit' or the web UI; " +
		"this file is overwritten on sync. -->"
	headerPrefix = "<!-- Managed by gridctl."
)

const (
	backupSuffix     = ".gridctl-backup-"
	backupTimeFormat = "20060102-150405"
	maxBackups       = 3
)

// hashScheme prefixes every stored hash so a future scheme change never
// presents as false drift (the pkg/pins lesson).
const hashScheme = "sha256:"

// contentHash returns the scheme-prefixed hash of CRLF-normalized content.
func contentHash(content string) string {
	sum := sha256.Sum256([]byte(normalizeNewlines(content)))
	return hashScheme + hex.EncodeToString(sum[:])
}

// normalizeNewlines converts CRLF to LF so editor line-ending churn never
// reads as drift.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// restoreCRLF converts content back to CRLF when the original file used
// CRLF, so a shim or block insertion never rewrites the user's line
// endings wholesale. Hashes always compare the normalized form.
func restoreCRLF(original, content string) string {
	if strings.Contains(original, "\r\n") {
		return strings.ReplaceAll(normalizeNewlines(content), "\n", "\r\n")
	}
	return content
}

// canonicalContentHash is the one hash expression used for the canonical
// file everywhere (SaveCanonical normalizes to a single trailing newline,
// so the trim keeps recorded and recomputed hashes identical).
func canonicalContentHash(content string) string {
	return contentHash(strings.TrimRight(normalizeNewlines(content), "\n"))
}

// renderDedicated builds the full content of a dedicated managed file:
// optional client-required frontmatter, the managed header, then the
// canonical body.
func renderDedicated(t Target, canonical string) string {
	var b strings.Builder
	if t.Frontmatter != "" {
		b.WriteString(t.Frontmatter)
	}
	b.WriteString(fileHeader)
	b.WriteString("\n\n")
	b.WriteString(strings.TrimRight(canonical, "\n"))
	b.WriteString("\n")
	return b.String()
}

// renderBlock builds the marker-delimited managed block.
func renderBlock(canonical string) string {
	var b strings.Builder
	b.WriteString(beginMarker)
	b.WriteString("\n")
	b.WriteString(blockHeader)
	b.WriteString("\n\n")
	b.WriteString(strings.TrimRight(canonical, "\n"))
	b.WriteString("\n")
	b.WriteString(endMarker)
	return b.String()
}

// lineAnchoredIndex finds marker occurrences that span a whole line of s
// (never mid-line, so prose that merely mentions a marker cannot read as
// a block boundary). Returns the first occurrence's offset (-1 if none)
// and the total count.
func lineAnchoredIndex(s, marker string) (first, count int) {
	first = -1
	offset := 0
	for {
		idx := strings.Index(s[offset:], marker)
		if idx < 0 {
			return first, count
		}
		abs := offset + idx
		atLineStart := abs == 0 || s[abs-1] == '\n'
		atLineEnd := abs+len(marker) == len(s) || s[abs+len(marker)] == '\n'
		if atLineStart && atLineEnd {
			if first < 0 {
				first = abs
			}
			count++
		}
		offset = abs + len(marker)
	}
}

// blockBounds locates the managed block in content. Returns the index of
// the BEGIN marker line start and the index just past the END marker.
// found is false when no BEGIN marker exists. A BEGIN without a matching
// END, or duplicated markers (a copy-pasted block), is corrupt: refuse
// rather than guess at the boundary.
func blockBounds(content string) (start, end int, found bool, err error) {
	norm := normalizeNewlines(content)
	beginIdx, beginCount := lineAnchoredIndex(norm, beginMarker)
	if beginCount == 0 {
		return 0, 0, false, nil
	}
	if beginCount > 1 {
		return 0, 0, false, fmt.Errorf("managed block is corrupt: %d %q markers found", beginCount, beginMarker)
	}
	endIdx, endCount := lineAnchoredIndex(norm[beginIdx:], endMarker)
	if endCount == 0 {
		return 0, 0, false, fmt.Errorf("managed block is corrupt: %q marker without %q", beginMarker, endMarker)
	}
	if endCount > 1 {
		return 0, 0, false, fmt.Errorf("managed block is corrupt: %d %q markers found", endCount, endMarker)
	}
	return beginIdx, beginIdx + endIdx + len(endMarker), true, nil
}

// extractBlockInner returns the content between the markers (exclusive),
// with the managed header line removed.
func extractBlockInner(content string) (string, bool, error) {
	norm := normalizeNewlines(content)
	start, end, found, err := blockBounds(norm)
	if err != nil || !found {
		return "", found, err
	}
	inner := norm[start+len(beginMarker) : end-len(endMarker)]
	return strings.TrimSpace(stripHeaderLines(inner)), true, nil
}

// upsertBlock replaces the managed block in content, or appends one when
// absent. force repairs a corrupt block by replacing everything from the
// BEGIN marker to the end of the file.
func upsertBlock(content, canonical string, force bool) (string, error) {
	norm := normalizeNewlines(content)
	block := renderBlock(canonical)

	start, end, found, err := blockBounds(norm)
	if err != nil {
		if !force {
			return "", err
		}
		// Repair: replace everything from the first line-anchored BEGIN
		// marker to the end of the file with a fresh block.
		start, _ = lineAnchoredIndex(norm, beginMarker)
		return strings.TrimRight(norm[:start], "\n") + trailingSep(norm[:start]) + block + "\n", nil
	}
	if !found {
		if strings.TrimSpace(norm) == "" {
			return block + "\n", nil
		}
		return strings.TrimRight(norm, "\n") + "\n\n" + block + "\n", nil
	}
	return norm[:start] + block + norm[end:], nil
}

// trailingSep returns the separator between preserved user content and an
// appended block: nothing when the prefix is empty, a blank line otherwise.
func trailingSep(prefix string) string {
	if strings.TrimSpace(prefix) == "" {
		return ""
	}
	return "\n\n"
}

// removeBlock strips the managed block from content, collapsing the blank
// lines the block insertion added.
func removeBlock(content string) (string, error) {
	norm := normalizeNewlines(content)
	start, end, found, err := blockBounds(norm)
	if err != nil || !found {
		return norm, err
	}
	before := strings.TrimRight(norm[:start], "\n")
	after := strings.TrimLeft(norm[end:], "\n")
	switch {
	case before == "":
		return after, nil
	case after == "":
		return before + "\n", nil
	default:
		return before + "\n\n" + after, nil
	}
}

// shimLine is the @-import directive referencing the canonical file. The
// canonical path itself identifies the line, so no trailing marker is
// needed (a marker could break the client's include parsing).
func shimLine(canonicalPath string) string {
	return "@" + canonicalPath
}

// hasShim reports whether any line in content is the shim directive.
func hasShim(content, canonicalPath string) bool {
	shim := shimLine(canonicalPath)
	for _, line := range strings.Split(normalizeNewlines(content), "\n") {
		if strings.TrimSpace(line) == shim {
			return true
		}
	}
	return false
}

// upsertShim inserts the shim directive as the first line when absent.
// The rest of the file is never reordered or rewritten.
func upsertShim(content, canonicalPath string) string {
	norm := normalizeNewlines(content)
	if hasShim(norm, canonicalPath) {
		return norm
	}
	if strings.TrimSpace(norm) == "" {
		return shimLine(canonicalPath) + "\n"
	}
	return shimLine(canonicalPath) + "\n\n" + norm
}

// removeShim deletes the shim directive line (and one adjacent blank line).
func removeShim(content, canonicalPath string) string {
	shim := shimLine(canonicalPath)
	lines := strings.Split(normalizeNewlines(content), "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == shim {
			if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == "" {
				i++
			}
			continue
		}
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

// stripHeaderLines removes managed-header comment lines.
func stripHeaderLines(content string) string {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), headerPrefix) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// stripManagedChrome recovers the user-visible body from a dedicated
// managed file: leading frontmatter (when the target requires one) and
// the managed header are removed. Used by adopt and import.
func stripManagedChrome(t Target, content string) string {
	norm := normalizeNewlines(content)
	if t.Frontmatter != "" && strings.HasPrefix(norm, "---\n") {
		if end := strings.Index(norm[4:], "\n---\n"); end >= 0 {
			norm = norm[4+end+len("\n---\n"):]
		}
	}
	return strings.TrimSpace(stripHeaderLines(norm))
}

// managedRegionHash returns the drift-comparison hash of the target's
// current managed region per strategy, and whether the region exists.
func managedRegionHash(t Target, content, canonicalPath string) (hash string, found bool, err error) {
	switch t.Strategy {
	case StrategyDedicatedFile:
		return contentHash(content), true, nil
	case StrategyImportShim:
		if !hasShim(content, canonicalPath) {
			return "", false, nil
		}
		return contentHash(shimLine(canonicalPath)), true, nil
	case StrategyBlock:
		inner, ok, err := extractBlockInner(content)
		if err != nil || !ok {
			return "", ok, err
		}
		return contentHash(inner), true, nil
	}
	return "", false, fmt.Errorf("unknown strategy %q", t.Strategy)
}

// atomicWriteFile writes data via a uniquely named temp file + rename in
// the target dir. Unique names keep concurrent writers from clobbering
// each other's in-flight temp file.
func atomicWriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("setting temp file permissions: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// createBackup copies path to a timestamped sibling before a write,
// pruning to maxBackups. Returns "" when the source does not exist.
// Mirrors pkg/provisioner's backup semantics (helpers there are
// unexported, so pkg/contexts keeps its own copy).
func createBackup(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading file for backup: %w", err)
	}
	backupPath := path + backupSuffix + time.Now().Format(backupTimeFormat)
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("writing backup: %w", err)
	}
	pruneBackups(path)
	return backupPath, nil
}

// pruneBackups keeps only the most recent maxBackups backups. Best-effort:
// a failed prune never fails the write it follows.
func pruneBackups(originalPath string) {
	dir := filepath.Dir(originalPath)
	prefix := filepath.Base(originalPath) + backupSuffix
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var backups []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, filepath.Join(dir, entry.Name()))
		}
	}
	if len(backups) <= maxBackups {
		return
	}
	sort.Strings(backups)
	for _, p := range backups[:len(backups)-maxBackups] {
		_ = os.Remove(p)
	}
}
