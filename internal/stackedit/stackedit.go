// Package stackedit holds the shared primitives for mutating stack.yaml
// safely: comment-preserving yaml.Node appends, per-path in-process locking,
// and crash-safe atomic writes. Both the API server's stack editors and the
// import CLI build on these so there is exactly one write discipline.
package stackedit

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// ErrServerNotFound is returned when a stack or override edit names a server
// that is not declared.
var ErrServerNotFound = errors.New("server not found")

// AppendResources appends one or more mapping snippets to the top-level
// sequence at key in source, in order. The yaml.Node round-trip preserves
// comments, key ordering, and unrelated formatting; a canonical re-emit from
// a Go struct would lose all three. Each snippet must parse to a single
// mapping. A null-valued or absent target sequence is replaced with a fresh
// sequence in place.
func AppendResources(source []byte, key string, snippets ...[]byte) ([]byte, error) {
	if len(snippets) == 0 {
		return nil, fmt.Errorf("no snippets provided")
	}

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

	seq := findOrCreateSequence(doc, key)
	if seq == nil {
		return nil, fmt.Errorf("locate %s sequence", key)
	}
	// Force block style: a sequence that was emptied in an earlier round trip
	// re-parses as flow ([]), and appending into a flow sequence would emit
	// the whole list on one line.
	seq.Style = 0
	for _, snippet := range snippets {
		var snippetDoc yaml.Node
		if err := yaml.Unmarshal(snippet, &snippetDoc); err != nil {
			return nil, fmt.Errorf("parse snippet yaml: %w", err)
		}
		if snippetDoc.Kind != yaml.DocumentNode || len(snippetDoc.Content) == 0 {
			return nil, fmt.Errorf("parse snippet yaml: empty")
		}
		item := snippetDoc.Content[0]
		if item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("parse snippet yaml: not a mapping")
		}
		seq.Content = append(seq.Content, item)
	}

	return encodeNode(&root)
}

// findOrCreateSequence returns the sequence node at key in the top-level
// mapping, creating one when the key is missing or its value is null/empty.
// Replacement happens in place so the mapping's existing key order survives.
func findOrCreateSequence(doc *yaml.Node, key string) *yaml.Node {
	if doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != key {
			continue
		}
		v := doc.Content[i+1]
		if v.Kind == yaml.SequenceNode {
			return v
		}
		// Null or any other non-sequence value: replace with an empty sequence
		// so the caller can append. yaml.v3 parses `mcp-servers:` (no value) as
		// a scalar null node, not a sequence.
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		doc.Content[i+1] = seq
		return seq
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	doc.Content = append(doc.Content, keyNode, seq)
	return seq
}

// pathLocks serializes read-verify-write cycles on a per-path basis within
// this process. External editors do not respect the lock; callers pair it
// with a pre-write hash re-check to catch those.
var pathLocks sync.Map // map[string]*sync.Mutex

// PathLock returns the in-process mutex guarding writes to path.
func PathLock(path string) *sync.Mutex {
	if m, ok := pathLocks.Load(path); ok {
		return m.(*sync.Mutex)
	}
	m, _ := pathLocks.LoadOrStore(path, &sync.Mutex{})
	return m.(*sync.Mutex)
}

// AtomicWrite writes data to path via a same-directory temp file + fsync +
// rename. A mid-write crash leaves the original file intact; a mid-rename
// crash on a POSIX filesystem leaves either the old or the new file at path,
// never a truncated mix. The original file's permissions are preserved.
func AtomicWrite(path string, data []byte) error {
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
	// file on some filesystems. Errors here are non-fatal: the write
	// succeeded; we only lose the crash-consistency guarantee.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// RemoveResourceByName removes the entry whose name: field equals name from
// the top-level sequence at key, preserving comments and ordering elsewhere.
// Returns an error when the sequence or the named entry is absent.
func RemoveResourceByName(source []byte, key, name string) ([]byte, error) {
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

	var seq *yaml.Node
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == key {
			seq = doc.Content[i+1]
			break
		}
	}
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("no %s sequence in stack", key)
	}

	for i, entry := range seq.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(entry.Content); j += 2 {
			if entry.Content[j].Value == "name" && entry.Content[j+1].Value == name {
				seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)
				return encodeNode(&root)
			}
		}
	}
	return nil, fmt.Errorf("server %q not found in %s", name, key)
}

// SetServerEnabled sets or clears enabled on the named mcp-servers entry.
// enabled false inserts enabled: false. enabled true removes the key so the
// file does not accumulate enabled: true. Comments and ordering are preserved.
// changed is false when the server is already in the requested state.
// ErrServerNotFound is returned for an unknown server.
func SetServerEnabled(source []byte, name string, enabled bool) ([]byte, bool, error) {
	root, doc, err := parseStackDocument(source)
	if err != nil {
		return nil, false, err
	}
	seq := mappingValue(doc, "mcp-servers")
	if seq == nil || seq.Kind != yaml.SequenceNode {
		return nil, false, fmt.Errorf("%w: %s", ErrServerNotFound, name)
	}
	entry := sequenceEntryByName(seq, name)
	if entry == nil {
		return nil, false, fmt.Errorf("%w: %s", ErrServerNotFound, name)
	}
	changed := setEnabledKey(entry, enabled)
	if !changed {
		return source, false, nil
	}
	out, err := encodeNode(root)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// SetOverrideEnabled sets enabled on mcp-servers.<name> in an override
// document. A missing file body is created. Other keys are preserved.
// Unlike the base stack, enabled: true is written explicitly: the override
// must be able to turn a base enabled: false back on.
func SetOverrideEnabled(source []byte, name string, enabled bool) ([]byte, bool, error) {
	if name == "" {
		return nil, false, fmt.Errorf("%w: empty name", ErrServerNotFound)
	}
	if len(bytes.TrimSpace(source)) == 0 {
		source = []byte("mcp-servers: {}\n")
	}
	root, doc, err := parseStackDocument(source)
	if err != nil {
		return nil, false, err
	}
	servers := findOrCreateMapping(doc, "mcp-servers")
	entry := findOrCreateMapping(servers, name)
	changed := setOverrideEnabledKey(entry, enabled)
	if !changed {
		return source, false, nil
	}
	out, err := encodeNode(root)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func parseStackDocument(source []byte) (*yaml.Node, *yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return nil, nil, fmt.Errorf("parse stack yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil, nil, fmt.Errorf("parse stack yaml: not a document")
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("parse stack yaml: top-level not a mapping")
	}
	return &root, doc, nil
}

func mappingValue(doc *yaml.Node, key string) *yaml.Node {
	if doc == nil || doc.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == key {
			return doc.Content[i+1]
		}
	}
	return nil
}

func sequenceEntryByName(seq *yaml.Node, name string) *yaml.Node {
	for _, entry := range seq.Content {
		if entry.Kind != yaml.MappingNode {
			continue
		}
		if mappingScalar(entry, "name") == name {
			return entry
		}
	}
	return nil
}

func mappingScalar(mapping *yaml.Node, key string) string {
	node := mappingValue(mapping, key)
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return node.Value
}

// setEnabledKey inserts enabled: false or removes the key when enabling.
// Returns whether the node changed.
func setEnabledKey(entry *yaml.Node, enabled bool) bool {
	idx := mappingKeyIndex(entry, "enabled")
	if enabled {
		if idx < 0 {
			return false
		}
		value := entry.Content[idx+1]
		if value.Kind == yaml.ScalarNode && (value.Value == "true" || value.Value == "True" || value.Value == "yes") {
			entry.Content = append(entry.Content[:idx], entry.Content[idx+2:]...)
			return true
		}
		if value.Kind == yaml.ScalarNode && (value.Value == "false" || value.Value == "False" || value.Value == "no") {
			entry.Content = append(entry.Content[:idx], entry.Content[idx+2:]...)
			return true
		}
		entry.Content = append(entry.Content[:idx], entry.Content[idx+2:]...)
		return true
	}
	if idx >= 0 {
		value := entry.Content[idx+1]
		if value.Kind == yaml.ScalarNode && (value.Value == "false" || value.Value == "False" || value.Value == "no") {
			return false
		}
		entry.Content[idx+1] = boolScalar("false")
		return true
	}
	entry.Content = append(entry.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"}, boolScalar("false"))
	return true
}

// setOverrideEnabledKey writes an explicit enabled bool, including true.
func setOverrideEnabledKey(entry *yaml.Node, enabled bool) bool {
	want := "false"
	if enabled {
		want = "true"
	}
	idx := mappingKeyIndex(entry, "enabled")
	if idx >= 0 {
		value := entry.Content[idx+1]
		if value.Kind == yaml.ScalarNode && value.Value == want {
			return false
		}
		entry.Content[idx+1] = boolScalar(want)
		return true
	}
	entry.Content = append(entry.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "enabled"}, boolScalar(want))
	return true
}

func boolScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: value}
}

func mappingKeyIndex(mapping *yaml.Node, key string) int {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func findOrCreateMapping(parent *yaml.Node, key string) *yaml.Node {
	if parent.Kind != yaml.MappingNode {
		parent.Kind = yaml.MappingNode
		parent.Tag = "!!map"
		parent.Content = nil
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value != key {
			continue
		}
		v := parent.Content[i+1]
		if v.Kind == yaml.MappingNode {
			return v
		}
		mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		parent.Content[i+1] = mapping
		return mapping
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mapping := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, keyNode, mapping)
	return mapping
}

// encodeNode marshals a patched document with the package-standard two-space
// indent, so the round-trip encoding never drifts between call sites.
func encodeNode(root *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("marshal stack yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("marshal stack yaml: %w", err)
	}
	return buf.Bytes(), nil
}
