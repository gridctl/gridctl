package api

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// setClientScope writes the per-client access profile (servers/tools allow-list)
// for profileKey into the stack YAML at path. It mirrors setServerTools: it
// serializes concurrent callers on the same path, detects external edits via a
// pre-read hash vs. pre-write re-read, and writes atomically.
//
// servers and tools are each tri-state: a nil pointer leaves that axis of the
// profile untouched (so a server-only edit never clobbers an operator-authored
// tool allow-list), while a non-nil pointer replaces it (an empty slice drops
// the key, which per config semantics means "no restriction on that axis").
//
// The update is a yaml.Node round-trip so comments, ordering, sibling profiles,
// clients.default, and unrelated keys all survive (Article IX). Returns
// errStackModified when the file changed between the initial read and the
// write, or a wrapped error on parse/IO failure.
func setClientScope(path, profileKey string, servers, tools *[]string) error {
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

	updated, err := patchClientScope(original, profileKey, servers, tools)
	if err != nil {
		return err
	}

	fireBetweenReadsHook()

	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("re-read stack file: %w", err)
	}
	if sha256.Sum256(current) != originalHash {
		return errStackModified
	}

	return atomicWrite(path, updated)
}

// patchClientScope rewrites the YAML source so that
// clients.profiles.<profileKey> carries the given servers and tools allow-lists.
// It creates the `clients:` mapping, its `profiles:` mapping, and the profile
// entry on demand without disturbing any existing keys (clients.default, other
// profiles, profile aliases).
//
// servers and tools are tri-state: a nil pointer leaves that axis untouched, so
// a server-only edit preserves an operator-authored tool allow-list. A non-nil
// pointer replaces the axis; an empty slice drops the key (per config
// semantics, "no restriction on that axis").
func patchClientScope(source []byte, profileKey string, servers, tools *[]string) ([]byte, error) {
	if profileKey == "" {
		return nil, fmt.Errorf("client profile key must not be empty")
	}
	if servers == nil && tools == nil {
		return nil, fmt.Errorf("at least one of servers or tools must be set")
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

	clientsNode, err := ensureChildMapping(doc, "clients")
	if err != nil {
		return nil, err
	}
	profilesNode, err := ensureChildMapping(clientsNode, "profiles")
	if err != nil {
		return nil, err
	}
	profileNode, err := ensureChildMapping(profilesNode, profileKey)
	if err != nil {
		return nil, err
	}

	if servers != nil {
		replaceOrInsertStringSeq(profileNode, "servers", *servers)
	}
	if tools != nil {
		replaceOrInsertStringSeq(profileNode, "tools", *tools)
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

// scalarNode builds a plain string scalar node.
func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// ensureChildMapping returns the mapping node at parent[key], creating it when
// absent and replacing an explicit-null value (e.g. a bare `clients:` line)
// with a fresh mapping. It errors when the key holds a non-null, non-mapping
// value so a malformed block is reported rather than silently overwritten.
func ensureChildMapping(parent *yaml.Node, key string) (*yaml.Node, error) {
	existing := findMappingValue(parent, key)
	if existing != nil {
		if existing.Kind == yaml.MappingNode {
			return existing, nil
		}
		// A bare `key:` parses to a null scalar; replace it with a mapping.
		if existing.Kind == yaml.ScalarNode && (existing.Tag == "!!null" || existing.Value == "") {
			existing.Kind = yaml.MappingNode
			existing.Tag = "!!map"
			existing.Value = ""
			existing.Content = nil
			return existing, nil
		}
		return nil, fmt.Errorf("%s is not a mapping", key)
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, scalarNode(key), node)
	return node, nil
}

// replaceOrInsertStringSeq sets mapping[key] to a block sequence of values,
// replacing an existing sequence or appending the key. An empty values slice
// removes the key entirely (so an empty allow-list is expressed as the absence
// of the field, matching the per-server tools: convention).
func replaceOrInsertStringSeq(mapping *yaml.Node, key string, values []string) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			if len(values) == 0 {
				mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
				return
			}
			mapping.Content[i+1] = toolsSequenceNode(values)
			return
		}
	}
	if len(values) == 0 {
		return
	}
	mapping.Content = append(mapping.Content, scalarNode(key), toolsSequenceNode(values))
}
