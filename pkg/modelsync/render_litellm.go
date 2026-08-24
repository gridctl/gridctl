package modelsync

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// RenderLiteLLM renders the router-only LiteLLM fragment: exactly one
// model_list entry (the auto-router), with complexity_router_default_model
// as a sibling of complexity_router_config under litellm_params per
// LiteLLM's documented placement. The renderer is pure and
// deterministic: fixed key order, maps emitted in sorted order, no
// timestamps. It never emits backends (the parent owns model_list
// inventory), router_settings, or any secret material.
func RenderLiteLLM(p *Policy, policyHash string) ([]byte, error) {
	defaultBackend := p.Tiers.byName(p.Router.DefaultTier)
	if defaultBackend == "" {
		return nil, fmt.Errorf("default_tier %q has no backend", p.Router.DefaultTier)
	}

	routerConfig := &yaml.Node{Kind: yaml.MappingNode}
	tiers := &yaml.Node{Kind: yaml.MappingNode}
	for _, tier := range tierOrder {
		appendKV(tiers, tier, scalarNode(p.Tiers.byName(tier)))
	}
	appendKV(routerConfig, "tiers", tiers)
	if len(p.Weights) > 0 {
		weights := &yaml.Node{Kind: yaml.MappingNode}
		for _, k := range sortedKeys(p.Weights) {
			appendKV(weights, k, floatNode(p.Weights[k]))
		}
		appendKV(routerConfig, "dimension_weights", weights)
	}
	for _, k := range sortedAnyKeys(p.Passthrough) {
		if k == "tiers" {
			continue // typed key wins; validation already errors on it
		}
		if k == "dimension_weights" && len(p.Weights) > 0 {
			continue // typed key wins
		}
		node, err := anyToNode(p.Passthrough[k])
		if err != nil {
			return nil, fmt.Errorf("passthrough.%s: %w", k, err)
		}
		appendKV(routerConfig, k, node)
	}

	params := &yaml.Node{Kind: yaml.MappingNode}
	appendKV(params, "model", scalarNode("auto_router/complexity_router"))
	appendKV(params, "complexity_router_default_model", scalarNode(defaultBackend))
	appendKV(params, "complexity_router_config", routerConfig)

	entry := &yaml.Node{Kind: yaml.MappingNode}
	appendKV(entry, "model_name", scalarNode(p.Router.EntryModel))
	appendKV(entry, "litellm_params", params)

	root := &yaml.Node{Kind: yaml.MappingNode}
	appendKV(root, "model_list", &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{entry}})

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "# MANAGED BY GRIDCTL - do not edit. Edit the models policy and re-run\n")
	fmt.Fprintf(&buf, "# 'gridctl models sync'. Source: models policy %q  policy-hash: %s\n", p.Name, policyHash)
	fmt.Fprintf(&buf, "# The router below references model_name values from the including\n")
	fmt.Fprintf(&buf, "# config's own model_list; this fragment never defines backends.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return nil, fmt.Errorf("encoding LiteLLM fragment: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("encoding LiteLLM fragment: %w", err)
	}
	return buf.Bytes(), nil
}

func scalarNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}

func floatNode(f float64) *yaml.Node {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0" // keep the implicit float tag; "!!float 0" is noise
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: s}
}

func appendKV(m *yaml.Node, key string, value *yaml.Node) {
	m.Content = append(m.Content, scalarNode(key), value)
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// anyToNode converts a decoded YAML value into a node tree with maps in
// sorted key order, so passthrough content renders deterministically.
func anyToNode(v any) (*yaml.Node, error) {
	switch val := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case string:
		return scalarNode(val), nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(val)}, nil
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(val)}, nil
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(val, 10)}, nil
	case float64:
		return floatNode(val), nil
	case map[string]any:
		node := &yaml.Node{Kind: yaml.MappingNode}
		for _, k := range sortedAnyKeys(val) {
			child, err := anyToNode(val[k])
			if err != nil {
				return nil, err
			}
			appendKV(node, k, child)
		}
		return node, nil
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range val {
			child, err := anyToNode(item)
			if err != nil {
				return nil, err
			}
			node.Content = append(node.Content, child)
		}
		return node, nil
	}
	return nil, fmt.Errorf("unsupported value type %T", v)
}
