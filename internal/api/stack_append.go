package api

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// resourceTypeToKey maps the request's `resourceType` field to the top-level
// stack-YAML key the new entry is appended under.
var resourceTypeToKey = map[string]string{
	"mcp-server": "mcp-servers",
	"resource":   "resources",
}

// patchAppendResource appends a single resource to the appropriate top-level
// sequence in source. The yaml.Node round-trip preserves comments, key
// ordering, and unrelated formatting — the canonical re-emit from a Go struct
// would lose all three.
//
// resourceType selects the target sequence ("mcp-server" → mcp-servers,
// "resource" → resources). snippet is the YAML body of the entry being
// appended; it must parse to a single mapping. A null-valued or absent target
// sequence is replaced with a fresh sequence in place.
func patchAppendResource(source []byte, resourceType string, snippet []byte) ([]byte, error) {
	key, ok := resourceTypeToKey[resourceType]
	if !ok {
		return nil, fmt.Errorf("unsupported resourceType: %s", resourceType)
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

	seq := findOrCreateSequence(doc, key)
	if seq == nil {
		return nil, fmt.Errorf("locate %s sequence", key)
	}
	seq.Content = append(seq.Content, item)

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
