// Package varscan finds exact stored secret values in working-tree or staged
// text without retaining unsanitized snippets.
package varscan

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/gridctl/gridctl/pkg/vault"
)

const (
	// MaxFileSize is the largest file scanned.
	MaxFileSize = 10 << 20
	// MaxFindingsPerFile bounds output for a single file.
	MaxFindingsPerFile  = 100
	minimumSecretLength = 8
	binarySampleSize    = 8 << 10
)

type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Key      string `json:"key"`
	Snippet  string `json:"snippet"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
}

type Skip struct {
	File   string `json:"file,omitempty"`
	Key    string `json:"key,omitempty"`
	Reason string `json:"reason"`
}
type Truncation struct {
	File  string `json:"file"`
	Limit int    `json:"limit"`
}
type Result struct {
	Complete    bool         `json:"complete"`
	Findings    []Finding    `json:"findings"`
	Skipped     []Skip       `json:"skipped"`
	Truncations []Truncation `json:"truncations"`
}

type Options struct {
	Paths  []string
	Staged bool
}
type candidate struct {
	key   string
	value []byte
}

// Scan snapshots candidates from vars and scans the selected content source.
func Scan(ctx context.Context, vars []vault.Variable, opts Options) (Result, error) {
	result := Result{Complete: true, Findings: []Finding{}, Skipped: []Skip{}, Truncations: []Truncation{}}
	candidates := candidatesFrom(vars, &result)
	if opts.Staged {
		if len(opts.Paths) > 0 {
			return result, fmt.Errorf("paths cannot be combined with --staged")
		}
		if err := scanStaged(ctx, candidates, &result); err != nil {
			result.Complete = false
			return result, err
		}
	} else {
		paths := opts.Paths
		if len(paths) == 0 {
			paths = []string{"."}
		}
		if err := scanWorking(ctx, paths, candidates, &result); err != nil {
			result.Complete = false
			return result, err
		}
	}
	sort.Slice(result.Findings, func(i, j int) bool {
		a, b := result.Findings[i], result.Findings[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Key < b.Key
	})
	sort.Slice(result.Skipped, func(i, j int) bool {
		a, b := result.Skipped[i], result.Skipped[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.Reason < b.Reason
	})
	sort.Slice(result.Truncations, func(i, j int) bool { return result.Truncations[i].File < result.Truncations[j].File })
	return result, nil
}

func candidatesFrom(vars []vault.Variable, result *Result) []candidate {
	var out []candidate
	for _, v := range vars {
		if !v.IsSecret || vault.IsInternalCredential(v.Key) {
			continue
		}
		if v.Value == "" {
			result.Skipped = append(result.Skipped, Skip{Key: v.Key, Reason: "empty value"})
			continue
		}
		if len([]byte(v.Value)) < minimumSecretLength {
			result.Skipped = append(result.Skipped, Skip{Key: v.Key, Reason: "value shorter than 8 bytes"})
			continue
		}
		out = append(out, candidate{v.Key, []byte(v.Value)})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].value) == len(out[j].value) {
			return out[i].key < out[j].key
		}
		return len(out[i].value) > len(out[j].value)
	})
	return out
}

func scanWorking(ctx context.Context, paths []string, candidates []candidate, result *Result) error {
	for _, requested := range paths {
		root, matcher := ignoreMatcher(requested)
		err := filepath.WalkDir(requested, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				if !d.IsDir() {
					result.Skipped = append(result.Skipped, Skip{File: path, Reason: "symlink"})
				}
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if d.IsDir() && (d.Name() == ".git" || d.Name() == ".gridctl") {
				return filepath.SkipDir
			}
			if matcher != nil && matcher.Match(strings.Split(rel, "/"), d.IsDir()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				result.Skipped = append(result.Skipped, Skip{File: rel, Reason: "ignored"})
				return nil
			}
			if d.IsDir() {
				return nil
			}
			return scanFile(path, rel, info.Size(), candidates, result)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func ignoreMatcher(requested string) (string, gitignore.Matcher) {
	start, err := filepath.Abs(requested)
	if err != nil {
		return requested, nil
	}
	if info, statErr := os.Stat(start); statErr == nil && !info.IsDir() {
		start = filepath.Dir(start)
	}
	repo, err := git.PlainOpenWithOptions(start, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return start, nil
	}
	wt, err := repo.Worktree()
	if err != nil {
		return start, nil
	}
	root := wt.Filesystem.Root()
	patterns, err := gitignore.ReadPatterns(wt.Filesystem, nil)
	if err != nil {
		return root, nil
	}
	return root, gitignore.NewMatcher(patterns)
}

func scanStaged(ctx context.Context, candidates []candidate, result *Result) error {
	repo, err := git.PlainOpenWithOptions(".", &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return fmt.Errorf("opening Git repository: %w", err)
	}
	index, err := repo.Storer.Index()
	if err != nil {
		return fmt.Errorf("reading Git index: %w", err)
	}
	headBlobs := map[string]string{}
	if head, headErr := repo.Head(); headErr == nil {
		if commit, commitErr := repo.CommitObject(head.Hash()); commitErr == nil {
			if tree, treeErr := commit.Tree(); treeErr == nil {
				files := tree.Files()
				_ = files.ForEach(func(file *object.File) error {
					headBlobs[file.Name] = file.Hash.String()
					return nil
				})
			}
		}
	}
	for _, entry := range index.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if headBlobs[entry.Name] == entry.Hash.String() {
			continue
		}
		blob, err := repo.BlobObject(entry.Hash)
		if err != nil {
			return fmt.Errorf("reading staged blob %s: %w", entry.Name, err)
		}
		if blob.Size > MaxFileSize {
			result.Skipped = append(result.Skipped, Skip{File: entry.Name, Reason: "file exceeds 10 MiB"})
			continue
		}
		r, err := blob.Reader()
		if err != nil {
			return err
		}
		err = scanReader(r, entry.Name, blob.Size, candidates, result)
		closeErr := r.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func scanFile(path, name string, size int64, candidates []candidate, result *Result) error {
	if size > MaxFileSize {
		result.Skipped = append(result.Skipped, Skip{File: name, Reason: "file exceeds 10 MiB"})
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return scanReader(f, name, size, candidates, result)
}

func scanReader(r io.Reader, name string, size int64, candidates []candidate, result *Result) error {
	br := bufio.NewReaderSize(io.LimitReader(r, MaxFileSize+1), binarySampleSize)
	sample, err := br.Peek(binarySampleSize)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return err
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		result.Skipped = append(result.Skipped, Skip{File: name, Reason: "binary"})
		return nil
	}
	s := bufio.NewScanner(br)
	s.Buffer(make([]byte, 64<<10), MaxFileSize)
	line := 0
	count := 0
	truncated := false
	for s.Scan() {
		line++
		spans := findSpans(s.Bytes(), candidates)
		sanitized := sanitize(s.Bytes(), spans)
		for _, span := range spans {
			if count >= MaxFindingsPerFile {
				truncated = true
				break
			}
			result.Findings = append(result.Findings, Finding{File: filepath.ToSlash(name), Line: line, Column: span.start + 1, Key: span.key, Snippet: boundedSnippet(sanitized, span.start), Code: "V001", Severity: "critical"})
			count++
		}
	}
	if err := s.Err(); err != nil {
		return err
	}
	if truncated {
		result.Truncations = append(result.Truncations, Truncation{File: filepath.ToSlash(name), Limit: MaxFindingsPerFile})
	}
	return nil
}

type span struct {
	start, end int
	key        string
}

func findSpans(line []byte, candidates []candidate) []span {
	var out []span
	for _, c := range candidates {
		for from := 0; from <= len(line)-len(c.value); {
			i := bytes.Index(line[from:], c.value)
			if i < 0 {
				break
			}
			start := from + i
			out = append(out, span{start, start + len(c.value), c.key})
			from = start + 1
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].start == out[j].start {
			if out[i].end == out[j].end {
				return out[i].key < out[j].key
			}
			return out[i].end > out[j].end
		}
		return out[i].start < out[j].start
	})
	return out
}
func sanitize(line []byte, spans []span) []byte {
	safe := append([]byte(nil), line...)
	for _, s := range spans {
		for i := s.start; i < s.end; i++ {
			safe[i] = '*'
		}
	}
	return safe
}
func boundedSnippet(line []byte, column int) string {
	const width = 80
	start := column - width/2
	if start < 0 {
		start = 0
	}
	end := start + width
	if end > len(line) {
		end = len(line)
	}
	return string(line[start:end])
}
