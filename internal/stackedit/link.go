package stackedit

import (
	"errors"
	"fmt"

	"github.com/gridctl/gridctl/pkg/config"
	"gopkg.in/yaml.v3"
)

// Sentinel errors for link-block edits, so callers can distinguish expected
// states (nothing declared) from a stack file that failed to parse.
var (
	ErrNoLinkBlock      = errors.New("no link block in stack")
	ErrEntryNotDeclared = errors.New("client not declared in link block")
)

// UpsertLinkEntry adds or replaces the entry for entry.Client in the
// top-level link: sequence, preserving comments, ordering, and the form of
// untouched entries. The link: sequence mixes scalar shorthands
// ("- cursor") and mappings; a slug-only entry is emitted back as the
// scalar form. The key is created when absent.
func UpsertLinkEntry(source []byte, entry config.LinkEntry) ([]byte, error) {
	if entry.Client == "" {
		return nil, fmt.Errorf("link entry client is required")
	}

	root, doc, err := parseStackDoc(source)
	if err != nil {
		return nil, err
	}

	seq, err := findOrCreateLinkSequence(doc)
	if err != nil {
		return nil, err
	}
	// Force block style; an emptied sequence re-parses as flow ([]).
	seq.Style = 0

	node := linkEntryNode(entry)
	if i := indexOfLinkEntry(seq, entry.Client); i >= 0 {
		seq.Content[i] = node
	} else {
		seq.Content = append(seq.Content, node)
	}

	return encodeNode(root)
}

// RemoveLinkEntry removes the entry for client from the top-level link:
// sequence. When the last entry goes, the link: key is dropped entirely so
// the file does not carry an empty block. Returns an error when the block
// or the entry is absent.
func RemoveLinkEntry(source []byte, client string) ([]byte, error) {
	root, doc, err := parseStackDoc(source)
	if err != nil {
		return nil, err
	}

	var seq *yaml.Node
	seqAt := -1
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "link" {
			seq = doc.Content[i+1]
			seqAt = i
			break
		}
	}
	if seq == nil || resolveAlias(seq).Kind != yaml.SequenceNode {
		return nil, ErrNoLinkBlock
	}
	seq = resolveAlias(seq)

	i := indexOfLinkEntry(seq, client)
	if i < 0 {
		return nil, fmt.Errorf("%w: %q", ErrEntryNotDeclared, client)
	}
	seq.Content = append(seq.Content[:i], seq.Content[i+1:]...)

	if len(seq.Content) == 0 {
		doc.Content = append(doc.Content[:seqAt], doc.Content[seqAt+2:]...)
	}

	return encodeNode(root)
}

// parseStackDoc unmarshals source and returns the document root plus its
// top-level mapping, with the same guards every patcher in this package
// applies.
func parseStackDoc(source []byte) (*yaml.Node, *yaml.Node, error) {
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

// findOrCreateLinkSequence returns the link: sequence, creating the key
// when absent and replacing a bare-null value in place. Unlike the generic
// findOrCreateSequence, any other value kind (an alias, a scalar, a
// mapping) is an error rather than a silent replacement — clobbering it
// would drop previously declared clients.
func findOrCreateLinkSequence(doc *yaml.Node) (*yaml.Node, error) {
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "link" {
			continue
		}
		v := resolveAlias(doc.Content[i+1])
		switch {
		case v.Kind == yaml.SequenceNode:
			return v, nil
		case v.Kind == yaml.ScalarNode && v.Tag == "!!null":
			seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			doc.Content[i+1] = seq
			return seq, nil
		default:
			return nil, fmt.Errorf("link: value is not a sequence")
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "link"}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	doc.Content = append(doc.Content, keyNode, seq)
	return seq, nil
}

// resolveAlias follows an alias node to its anchor target; other nodes pass
// through. yaml.v3 resolves aliases during decode, so the edit layer must
// match what config.LoadStack saw.
func resolveAlias(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

// indexOfLinkEntry returns the position of client in the link sequence,
// matching the scalar shorthand, the mapping form, and aliases to either,
// or -1.
func indexOfLinkEntry(seq *yaml.Node, client string) int {
	for i, item := range seq.Content {
		switch resolved := resolveAlias(item); resolved.Kind {
		case yaml.ScalarNode:
			if resolved.Value == client {
				return i
			}
		case yaml.MappingNode:
			for j := 0; j+1 < len(resolved.Content); j += 2 {
				if resolved.Content[j].Value == "client" && resolveAlias(resolved.Content[j+1]).Value == client {
					return i
				}
			}
		}
	}
	return -1
}

// linkEntryNode builds the yaml node for an entry: the scalar shorthand for
// a slug-only entry, otherwise a mapping with keys in canonical order.
func linkEntryNode(entry config.LinkEntry) *yaml.Node {
	if entry.IsShorthand() {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: entry.Client}
	}
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	add := func(key, value string) {
		if value == "" {
			return
		}
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
		)
	}
	add("client", entry.Client)
	add("group", entry.Group)
	add("client_id", entry.ClientID)
	add("name", entry.Name)
	return m
}
