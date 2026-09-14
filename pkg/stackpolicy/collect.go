package stackpolicy

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"gopkg.in/yaml.v3"
)

const (
	maxFileBytes     = 1 << 20
	maxGraphBytes    = 4 << 20
	maxGraphFiles    = 12
	maxExtendsDepth  = 10
	entryAliasPrefix = "input-"
)

type capturedFile struct {
	alias string
	rel   string
	bytes []byte
	ident fileID
}

type snapshot struct {
	root      string
	policyID  fileID
	hasPolicy bool
	files     []capturedFile
}

func collectCandidate(ctx context.Context, stackPath string, policyIdent fileID, hasPolicy bool) (*snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stackPath == "" {
		return nil, codeErr("input-unreadable")
	}
	abs, err := filepath.Abs(stackPath)
	if err != nil {
		return nil, codeErr("input-unreadable")
	}
	rootDir := filepath.Dir(abs)
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return nil, codeErr("input-unreadable")
	}
	defer func() { _ = root.Close() }()

	rel, err := confinedRel(rootDir, abs)
	if err != nil {
		return nil, err
	}
	snap := &snapshot{
		root:      rootDir,
		policyID:  policyIdent,
		hasPolicy: hasPolicy,
	}
	visited := map[string]struct{}{}
	if err := snap.readChain(ctx, root, rel, visited, 0); err != nil {
		return nil, err
	}
	return snap, nil
}

func (s *snapshot) readChain(ctx context.Context, root *os.Root, rel string, visited map[string]struct{}, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." {
		return codeErr("input-unreadable")
	}
	if _, ok := visited[rel]; ok {
		return codeErr("extends-cycle")
	}
	if len(s.files) >= maxGraphFiles {
		return codeErr("extends-limit")
	}
	data, ident, err := s.readRootRegular(ctx, root, rel, len(s.files) == 0)
	if err != nil {
		return err
	}
	var total int
	for _, f := range s.files {
		total += len(f.bytes)
	}
	if total+len(data) > maxGraphBytes {
		return codeErr("extends-limit")
	}
	visited[rel] = struct{}{}
	alias := entryAliasPrefix + itoa(len(s.files))
	s.files = append(s.files, capturedFile{
		alias: alias,
		rel:   rel,
		bytes: data,
		ident: ident,
	})
	extends, err := extractExtends(data)
	if err != nil {
		return err
	}
	if extends == "" {
		return nil
	}
	if depth >= maxExtendsDepth {
		return codeErr("extends-depth")
	}
	parentRel, err := resolveExtendsRel(rel, extends)
	if err != nil {
		return err
	}
	return s.readChain(ctx, root, parentRel, visited, depth+1)
}

func (s *snapshot) readRootRegular(ctx context.Context, root *os.Root, rel string, entry bool) ([]byte, fileID, error) {
	if err := ctx.Err(); err != nil {
		return nil, fileID{}, err
	}
	f, err := openThrough(root, rel)
	if err != nil {
		if os.IsNotExist(err) {
			if entry {
				return nil, fileID{}, codeErr("input-unreadable")
			}
			return nil, fileID{}, codeErr("extends-missing")
		}
		if coded := errorCode(err); coded != "input-invalid" && strings.HasPrefix(coded, "extends-") {
			return nil, fileID{}, err
		}
		mapped := mapOpenError(err)
		if errorCode(mapped) == "extends-missing" && entry {
			return nil, fileID{}, codeErr("input-unreadable")
		}
		return nil, fileID{}, mapped
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, fileID{}, codeErr("input-unreadable")
	}
	if !st.Mode().IsRegular() {
		return nil, fileID{}, codeErr("extends-nonregular")
	}
	if st.Size() > maxFileBytes {
		return nil, fileID{}, codeErr("extends-limit")
	}
	ident, identOK := identFromFile(f)
	if !identOK {
		ident, identOK = identOf(st)
	}
	if err := rejectPolicyReuse(s.hasPolicy, identOK, s.policyID, ident); err != nil {
		return nil, fileID{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, fileID{}, codeErr("input-unreadable")
	}
	if int64(len(data)) > maxFileBytes {
		return nil, fileID{}, codeErr("extends-limit")
	}
	if err := ctx.Err(); err != nil {
		return nil, fileID{}, err
	}
	return append([]byte(nil), data...), ident, nil
}

func rejectPolicyReuse(hasPolicy, identOK bool, policyID, candidateID fileID) error {
	if !hasPolicy {
		return nil
	}
	if !identOK || candidateID == policyID {
		return codeErr("extends-policy")
	}
	return nil
}

func openThrough(root *os.Root, rel string) (*os.File, error) {
	rel = filepath.ToSlash(filepath.Clean(rel))
	if rel == "." || rel == "" {
		return nil, codeErr("input-unreadable")
	}
	parts := strings.Split(rel, "/")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" && part != "." {
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		return nil, codeErr("input-unreadable")
	}
	var opened []*os.Root
	defer func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].Close()
		}
	}()
	cur := root
	for i, part := range filtered {
		info, err := cur.Lstat(part)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, codeErr("extends-symlink")
		}
		if i == len(filtered)-1 {
			if !info.Mode().IsRegular() {
				return nil, codeErr("extends-nonregular")
			}
			if info.Size() > maxFileBytes {
				return nil, codeErr("extends-limit")
			}
			return cur.OpenFile(part, os.O_RDONLY|openNoFollow, 0)
		}
		if !info.IsDir() {
			return nil, codeErr("extends-nonregular")
		}
		next, err := cur.OpenRoot(part)
		if err != nil {
			return nil, err
		}
		opened = append(opened, next)
		cur = next
	}
	return nil, codeErr("input-unreadable")
}

func mapOpenError(err error) error {
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return codeErr("extends-missing")
	}
	msg := err.Error()
	if strings.Contains(msg, "escaped") || strings.Contains(msg, "out of root") || strings.Contains(msg, "path escapes") {
		return codeErr("extends-escape")
	}
	if strings.Contains(strings.ToLower(msg), "symlink") || strings.Contains(msg, "too many levels") {
		return codeErr("extends-symlink")
	}
	return codeErr("input-unreadable")
}

func confinedRel(rootDir, target string) (string, error) {
	rel, err := filepath.Rel(rootDir, target)
	if err != nil {
		return "", codeErr("extends-escape")
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", codeErr("extends-escape")
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == ".." {
			return "", codeErr("extends-escape")
		}
	}
	return filepath.ToSlash(rel), nil
}

func resolveExtendsRel(childRel, extends string) (string, error) {
	extends = strings.TrimSpace(extends)
	if extends == "" {
		return "", codeErr("extends-missing")
	}
	if config.ContainsExpansion(extends) {
		return "", codeErr("extends-dynamic")
	}
	if strings.Contains(extends, "://") || strings.HasPrefix(extends, "//") {
		return "", codeErr("extends-remote")
	}
	if filepath.IsAbs(extends) || strings.HasPrefix(extends, "/") || strings.HasPrefix(extends, "~") {
		return "", codeErr("extends-absolute")
	}
	joined := filepath.Join(filepath.FromSlash(path.Dir(childRel)), filepath.FromSlash(extends))
	clean := filepath.Clean(joined)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || filepath.IsAbs(clean) {
		return "", codeErr("extends-escape")
	}
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." {
			return "", codeErr("extends-escape")
		}
	}
	return filepath.ToSlash(clean), nil
}

func extractExtends(data []byte) (string, error) {
	doc, err := decodeSingleDocument(data)
	if err != nil {
		return "", err
	}
	root, err := documentMapping(doc)
	if err != nil {
		return "", err
	}
	n := mappingValue(root, "extends")
	if n == nil || isNull(n) {
		return "", nil
	}
	if n.Kind != yaml.ScalarNode {
		return "", codeErr("input-invalid")
	}
	if config.ContainsExpansion(n.Value) {
		return "", codeErr("extends-dynamic")
	}
	return n.Value, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}


