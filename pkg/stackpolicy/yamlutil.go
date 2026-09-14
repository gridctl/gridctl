package stackpolicy

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/gridctl/gridctl/pkg/config"
	"gopkg.in/yaml.v3"
)

const (
	maxYAMLDepth = 64
	maxYAMLNodes = 20000
)

type codedError struct {
	code string
}

func (e *codedError) Error() string { return e.code }

func codeErr(code string) error { return &codedError{code: code} }

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	var ce *codedError
	if ok := asCoded(err, &ce); ok {
		return ce.code
	}
	return "input-invalid"
}

func asCoded(err error, target **codedError) bool {
	if err == nil {
		return false
	}
	ce, ok := err.(*codedError)
	if !ok {
		return false
	}
	*target = ce
	return true
}

func decodeSingleDocument(data []byte) (*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if err == io.EOF {
			return nil, codeErr("empty-document")
		}
		return nil, codeErr("yaml-invalid")
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, codeErr("multiple-documents")
		}
		return nil, codeErr("yaml-invalid")
	}
	if err := walkLimits(&doc, 0, 0); err != nil {
		return nil, err
	}
	if err := rejectDuplicateKeys(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func walkLimits(n *yaml.Node, depth, seen int) error {
	if n == nil {
		return nil
	}
	if depth > maxYAMLDepth {
		return codeErr("yaml-depth")
	}
	seen++
	if seen > maxYAMLNodes {
		return codeErr("yaml-nodes")
	}
	cur := n
	if cur.Kind == yaml.AliasNode {
		cur = cur.Alias
		if cur == nil {
			return codeErr("yaml-invalid")
		}
	}
	nextDepth := depth + 1
	count := seen
	for _, c := range cur.Content {
		if err := walkLimitsCounted(c, nextDepth, &count); err != nil {
			return err
		}
	}
	return nil
}

func walkLimitsCounted(n *yaml.Node, depth int, seen *int) error {
	if n == nil {
		return nil
	}
	if depth > maxYAMLDepth {
		return codeErr("yaml-depth")
	}
	*seen++
	if *seen > maxYAMLNodes {
		return codeErr("yaml-nodes")
	}
	cur := n
	if cur.Kind == yaml.AliasNode {
		cur = cur.Alias
		if cur == nil {
			return codeErr("yaml-invalid")
		}
	}
	for _, c := range cur.Content {
		if err := walkLimitsCounted(c, depth+1, seen); err != nil {
			return err
		}
	}
	return nil
}

func rejectDuplicateKeys(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	cur := resolveAlias(n)
	switch cur.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, c := range cur.Content {
			if err := rejectDuplicateKeys(c); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		if len(cur.Content)%2 != 0 {
			return codeErr("yaml-invalid")
		}
		seen := make(map[string]struct{}, len(cur.Content)/2)
		for i := 0; i < len(cur.Content); i += 2 {
			key := resolveAlias(cur.Content[i])
			if key.Kind != yaml.ScalarNode {
				return codeErr("yaml-invalid")
			}
			if _, ok := seen[key.Value]; ok {
				return codeErr("duplicate-key")
			}
			seen[key.Value] = struct{}{}
			if err := rejectDuplicateKeys(cur.Content[i+1]); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectInterpolation(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	cur := resolveAlias(n)
	if cur.Kind == yaml.ScalarNode && config.ContainsExpansion(cur.Value) {
		return codeErr("interpolation")
	}
	for _, c := range cur.Content {
		if err := rejectInterpolation(c); err != nil {
			return err
		}
	}
	return nil
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

func documentMapping(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil {
		return nil, codeErr("yaml-invalid")
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, codeErr("yaml-invalid")
	}
	m := resolveAlias(doc.Content[0])
	if m.Kind != yaml.MappingNode {
		return nil, codeErr("yaml-invalid")
	}
	return m, nil
}

func mappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := resolveAlias(m.Content[i])
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return resolveAlias(m.Content[i+1])
		}
	}
	return nil
}

func mappingHasKey(m *yaml.Node, key string) bool {
	if m == nil || m.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := resolveAlias(m.Content[i])
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return true
		}
	}
	return false
}

func mappingKeys(m *yaml.Node) []string {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := resolveAlias(m.Content[i])
		if k.Kind == yaml.ScalarNode {
			keys = append(keys, k.Value)
		}
	}
	return keys
}

func isNull(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	return n.Tag == "!!null"
}

func scalarString(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	if n.Tag == "!!null" {
		return "", false
	}
	return n.Value, true
}

func scalarBool(n *yaml.Node) (value bool, ok bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return false, false
	}
	switch strings.ToLower(n.Value) {
	case "true", "yes", "y", "on":
		if n.Tag == "!!str" {
			return false, false
		}
		return true, true
	case "false", "no", "n", "off":
		if n.Tag == "!!str" {
			return false, false
		}
		return false, true
	}
	return false, false
}

func nodeDynamic(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	cur := resolveAlias(n)
	if cur.Kind == yaml.ScalarNode {
		return config.ContainsExpansion(cur.Value)
	}
	for _, c := range cur.Content {
		if nodeDynamic(c) {
			return true
		}
	}
	return false
}

func locOf(n *yaml.Node, source, path string) Location {
	loc := Location{Source: source, Path: path}
	if n != nil {
		loc.Line = n.Line
		loc.Column = n.Column
	}
	return loc
}

func decodeKnownFields(data []byte, dest any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(dest); err != nil {
		return fmt.Errorf("known-fields")
	}
	return nil
}
