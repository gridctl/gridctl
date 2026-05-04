package api

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

// Sentinel errors for the tool-whitelist editor. Callers translate these into
// HTTP status codes; the YAML helper itself is transport-agnostic.
var (
	errServerNotFound = errors.New("mcp server not found in stack")
	errStackModified  = errors.New("stack file was modified on disk since read")
	errStackFileEmpty = errors.New("no stack file configured")
)

// stackFileLocks serializes read-verify-write cycles on a per-path basis. The
// daemon typically operates on one stack file, so contention is low; the map
// keeps the door open for multi-stack futures without a global chokepoint.
var stackFileLocks sync.Map // map[string]*sync.Mutex

// stackEditBetweenReadsHook fires after the initial read and before the
// pre-write re-read on every read-verify-write cycle in this package
// (setServerTools, handleStackAppend). Production code leaves it nil; tests
// set it to simulate an external edit landing in the narrow window between
// the two reads. The atomic.Value wrapper keeps the race detector quiet when
// parallel tests run.
var stackEditBetweenReadsHook atomic.Value // stores func()

func swapBetweenReadsHook(fn func()) func() {
	prev := stackEditBetweenReadsHook.Load()
	if fn == nil {
		stackEditBetweenReadsHook.Store((func())(nil))
	} else {
		stackEditBetweenReadsHook.Store(fn)
	}
	if prev == nil {
		return nil
	}
	return prev.(func())
}

func fireBetweenReadsHook() {
	v := stackEditBetweenReadsHook.Load()
	if v == nil {
		return
	}
	fn, _ := v.(func())
	if fn != nil {
		fn()
	}
}

func stackFileLock(path string) *sync.Mutex {
	if m, ok := stackFileLocks.Load(path); ok {
		return m.(*sync.Mutex)
	}
	m, _ := stackFileLocks.LoadOrStore(path, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// setServerTools updates the `tools:` field on the named MCP server in the
// stack YAML at path. It serializes concurrent callers on the same path,
// detects external edits via a pre-read hash vs. pre-write re-read, and writes
// atomically (temp file + fsync + rename) so a mid-write crash leaves the
// original file intact.
//
// The update is done via yaml.Node round-tripping so top-level comments,
// ordering, and unrelated scalar formatting survive. tools must contain only
// non-empty strings; the caller is responsible for validating tool names
// against the server's discovered tools.
//
// Returns errServerNotFound when the server name is absent, errStackModified
// when the on-disk file changed between the initial read and the atomic write,
// or a wrapped filesystem error on I/O failure.
func setServerTools(path, serverName string, tools []string) error {
	if path == "" {
		return errStackFileEmpty
	}

	mu := stackFileLock(path)
	mu.Lock()
	defer mu.Unlock()

	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read stack file: %w", err)
	}
	originalHash := sha256.Sum256(original)

	updated, err := patchServerTools(original, serverName, tools)
	if err != nil {
		return err
	}

	fireBetweenReadsHook()

	// Re-read right before write to catch any external edit that sneaked in
	// between our initial read and the write. With the per-path mutex this is
	// a tight window, but external editors (vim, git) do not respect our lock.
	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read stack file: %w", err)
	}
	if sha256.Sum256(current) != originalHash {
		return errStackModified
	}

	return atomicWrite(path, updated)
}

// patchServerTools rewrites the given YAML source with tools set to the
// provided list for the named server. The yaml.Node round-trip keeps line
// comments and ordering; only the target tools: sequence is replaced.
func patchServerTools(source []byte, serverName string, tools []string) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return nil, fmt.Errorf("parse stack yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, fmt.Errorf("parse stack yaml: not a document")
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("parse stack yaml: top-level not a mapping")
	}

	serversSeq := findMappingValue(doc, "mcp-servers")
	if serversSeq == nil || serversSeq.Kind != yaml.SequenceNode {
		return nil, errServerNotFound
	}

	var targetServer *yaml.Node
	for _, entry := range serversSeq.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		nameNode := findMappingValue(entry, "name")
		if nameNode != nil && nameNode.Value == serverName {
			targetServer = entry
			break
		}
	}
	if targetServer == nil {
		return nil, errServerNotFound
	}

	if err := replaceOrInsertTools(targetServer, tools); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, fmt.Errorf("marshal stack yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("marshal stack yaml: %w", err)
	}
	return buf.Bytes(), nil
}

// findMappingValue returns the value node associated with key in a mapping,
// or nil when the key is absent. Mapping node Content alternates key/value
// nodes, so we iterate by two.
func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// replaceOrInsertTools sets the server mapping's tools: field. An empty
// whitelist removes the field (matching the YAML semantics that "no tools:
// key" means "expose all tools"). A non-empty whitelist replaces the existing
// sequence or appends a new key to the end of the mapping.
func replaceOrInsertTools(server *yaml.Node, tools []string) error {
	if server.Kind != yaml.MappingNode {
		return fmt.Errorf("server entry is not a mapping")
	}

	for i := 0; i+1 < len(server.Content); i += 2 {
		if server.Content[i].Value == "tools" {
			if len(tools) == 0 {
				// Drop the key/value pair entirely so the YAML no longer
				// carries a now-meaningless tools: field.
				server.Content = append(server.Content[:i], server.Content[i+2:]...)
				return nil
			}
			server.Content[i+1] = toolsSequenceNode(tools)
			return nil
		}
	}

	if len(tools) == 0 {
		// Nothing to do — already omitted.
		return nil
	}

	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tools"}
	server.Content = append(server.Content, keyNode, toolsSequenceNode(tools))
	return nil
}

// toolsSequenceNode builds a flow-preserving scalar sequence for the provided
// whitelist. We emit a block sequence — the canonical style in existing stack
// files — so diffs stay small when the tools list grows over time.
func toolsSequenceNode(tools []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	seq.Content = make([]*yaml.Node, 0, len(tools))
	for _, t := range tools {
		seq.Content = append(seq.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Tag:   "!!str",
			Value: t,
		})
	}
	return seq
}

// atomicWrite writes data to path via a same-directory temp file + fsync +
// rename. A mid-write crash leaves the original file intact; a mid-rename
// crash on a POSIX filesystem leaves either the old or the new file at path,
// never a truncated mix.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}

	// Preserve existing file mode when possible. A fresh CreateTemp uses 0600
	// by default; we want whatever perms the original had so operators who set
	// group-readable stacks keep that property.
	if info, err := os.Stat(path); err == nil {
		_ = os.Chmod(tmpName, info.Mode().Perm())
	}

	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}

	// fsync the parent directory so the rename itself is durable. Without
	// this, a power loss immediately after rename can revert to the original
	// file on some filesystems. Errors here are non-fatal — the write
	// succeeded; we only lose the crash-consistency guarantee.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
