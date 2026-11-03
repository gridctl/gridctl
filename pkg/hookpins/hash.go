package hookpins

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gridctl/gridctl/pkg/skillpins"
)

// hashBytes is the SHA-256 hex digest of data, without a scheme prefix.
// definition_hash is this digest of hook.json. File digests use the same
// function so a composite hash binds the bytes a reviewer saw.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// digestTree hashes hook.json and every installed file that participates
// in the pin. Excluded: .origin.json, dot-prefixed entries, .tmp files,
// symlinks, irregular files, and hook.json.pre-* backups.
func digestTree(dir string) (definitionHash string, document string, files []skillpins.FileDigest, err error) {
	hookPath := filepath.Join(dir, "hook.json")
	raw, err := os.ReadFile(hookPath) // #nosec G304 -- fixed name inside a hook directory the caller named
	if err != nil {
		return "", "", nil, fmt.Errorf("hookpins: %w: reading hook.json: %v", ErrDigestUnavailable, err)
	}
	definitionHash = hashBytes(raw)
	document = string(raw)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("hookpins: %w: walking %s: %v", ErrDigestUnavailable, path, walkErr)
		}
		if d == nil || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if !digestedHookFile(rel, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		data, rerr := os.ReadFile(path) // #nosec G304 -- path is inside the hook directory; symlinks are excluded
		if rerr != nil {
			return fmt.Errorf("hookpins: %w: reading %s: %v", ErrDigestUnavailable, rel, rerr)
		}
		files = append(files, skillpins.FileDigest{Path: rel, Digest: hashBytes(data)})
		return nil
	})
	if err != nil {
		return "", "", nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return definitionHash, document, files, nil
}

func digestedHookFile(rel string, d fs.DirEntry) bool {
	if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
		return false
	}
	if rel == "hook.json" || strings.HasPrefix(rel, "hook.json.") {
		return false
	}
	if strings.HasSuffix(rel, ".tmp") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func composite(definitionHash string, files []skillpins.FileDigest) string {
	return skillpins.CompositeHash(definitionHash, files)
}
