package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"
)

// stackPersistDelta is the persist subblock of a PATCH /api/stack/telemetry
// request. Each *bool is tri-state: nil = no change, &true/&false = set the
// stack-global default. Stack-global persistence is binary (the on-disk YAML
// stores plain bool); the *bool is here so JSON `null` and absence both
// surface as "no change" rather than "set to false".
type stackPersistDelta struct {
	Logs    *bool `json:"logs,omitempty"`
	Metrics *bool `json:"metrics,omitempty"`
	Traces  *bool `json:"traces,omitempty"`
}

// retentionDelta is the retention subblock of a PATCH /api/stack/telemetry
// request. Each *int is "no change" when nil, "set to value" when non-nil.
type retentionDelta struct {
	MaxSizeMB  *int `json:"max_size_mb,omitempty"`
	MaxBackups *int `json:"max_backups,omitempty"`
	MaxAgeDays *int `json:"max_age_days,omitempty"`
}

// stackTelemetryRequest is the wire shape for PATCH /api/stack/telemetry.
type stackTelemetryRequest struct {
	Persist   *stackPersistDelta `json:"persist,omitempty"`
	Retention *retentionDelta    `json:"retention,omitempty"`
}

// hasChanges reports whether at least one field is non-nil. A request with
// neither persist nor retention is rejected upstream as "nothing to do".
func (r stackTelemetryRequest) hasChanges() bool {
	if r.Persist != nil && (r.Persist.Logs != nil || r.Persist.Metrics != nil || r.Persist.Traces != nil) {
		return true
	}
	if r.Retention != nil && (r.Retention.MaxSizeMB != nil || r.Retention.MaxBackups != nil || r.Retention.MaxAgeDays != nil) {
		return true
	}
	return false
}

// serverPersistOp expresses the four states a per-server signal override can
// transition to in a single PATCH: leave it alone, clear it (revert to
// inheriting the stack-global), or set an explicit true/false override.
type serverPersistOp int

const (
	serverPersistNoop serverPersistOp = iota
	serverPersistClear
	serverPersistTrue
	serverPersistFalse
)

// serverPersistDelta captures the per-signal ops for a single server PATCH.
type serverPersistDelta struct {
	Logs    serverPersistOp
	Metrics serverPersistOp
	Traces  serverPersistOp
}

// hasOps reports whether the delta would mutate any signal.
func (d serverPersistDelta) hasOps() bool {
	return d.Logs != serverPersistNoop || d.Metrics != serverPersistNoop || d.Traces != serverPersistNoop
}

// serverTelemetryRequest is the wire shape for PATCH
// /api/mcp-servers/{name}/telemetry. Persist is captured raw so the handler
// can distinguish three states that *bool cannot:
//
//  1. key absent          → no change
//  2. persist: null       → remove the entire per-server telemetry block
//  3. persist: { ... }    → per-signal ops, where each signal independently
//     follows the same three-state rule (absent / null / bool).
type serverTelemetryRequest struct {
	Persist json.RawMessage `json:"persist"`
}

// parseServerPersist resolves the raw persist value into a delta and a
// "clearAll" flag. clearAll==true means the request body had `persist: null`,
// which deletes the entire telemetry mapping from the server entry.
func parseServerPersist(raw json.RawMessage) (delta serverPersistDelta, clearAll bool, err error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return delta, false, nil
	}
	if string(trimmed) == "null" {
		return delta, true, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return delta, false, fmt.Errorf("persist must be an object or null: %w", err)
	}

	parseSignal := func(name string) (serverPersistOp, error) {
		v, ok := fields[name]
		if !ok {
			return serverPersistNoop, nil
		}
		v = bytes.TrimSpace(v)
		if string(v) == "null" {
			return serverPersistClear, nil
		}
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			return serverPersistNoop, fmt.Errorf("persist.%s must be true, false, or null", name)
		}
		if b {
			return serverPersistTrue, nil
		}
		return serverPersistFalse, nil
	}

	if delta.Logs, err = parseSignal("logs"); err != nil {
		return delta, false, err
	}
	if delta.Metrics, err = parseSignal("metrics"); err != nil {
		return delta, false, err
	}
	if delta.Traces, err = parseSignal("traces"); err != nil {
		return delta, false, err
	}
	return delta, false, nil
}

// patchStackTelemetry rewrites source with the requested updates to the
// top-level telemetry mapping. The yaml.Node round-trip preserves comments
// and key ordering across both the affected and the unaffected parts of the
// document, mirroring patchServerTools / patchAppendResource.
//
// A nil persist or retention pointer leaves the corresponding subblock alone.
// Within each subblock, only the non-nil leaf fields are mutated; the rest
// retain their existing on-disk values. The telemetry block is created if
// absent; subblocks (persist, retention) are created lazily as needed.
func patchStackTelemetry(source []byte, persist *stackPersistDelta, retention *retentionDelta) ([]byte, error) {
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

	if persist != nil || retention != nil {
		telemetryNode := findOrCreateMapping(doc, "telemetry")
		if telemetryNode == nil {
			return nil, fmt.Errorf("locate telemetry mapping")
		}
		if persist != nil {
			applyStackPersist(telemetryNode, persist)
		}
		if retention != nil {
			applyStackRetention(telemetryNode, retention)
		}
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

func applyStackPersist(telemetry *yaml.Node, delta *stackPersistDelta) {
	persistNode := findOrCreateMapping(telemetry, "persist")
	if persistNode == nil {
		return
	}
	if delta.Logs != nil {
		setMappingBool(persistNode, "logs", *delta.Logs)
	}
	if delta.Metrics != nil {
		setMappingBool(persistNode, "metrics", *delta.Metrics)
	}
	if delta.Traces != nil {
		setMappingBool(persistNode, "traces", *delta.Traces)
	}
}

func applyStackRetention(telemetry *yaml.Node, delta *retentionDelta) {
	retNode := findOrCreateMapping(telemetry, "retention")
	if retNode == nil {
		return
	}
	if delta.MaxSizeMB != nil {
		setMappingInt(retNode, "max_size_mb", *delta.MaxSizeMB)
	}
	if delta.MaxBackups != nil {
		setMappingInt(retNode, "max_backups", *delta.MaxBackups)
	}
	if delta.MaxAgeDays != nil {
		setMappingInt(retNode, "max_age_days", *delta.MaxAgeDays)
	}
}

// patchServerTelemetry rewrites source with the requested updates to a single
// MCP server's telemetry mapping. It mirrors patchServerTools: locate the
// named server in mcp-servers, mutate the target node tree in place, and
// re-emit via yaml.Encoder so comments and key ordering elsewhere in the
// document survive untouched.
//
// Behavior:
//   - clearAll=true: delete the server's `telemetry:` key entirely
//     (matches the "remove the block" idiom of replaceOrInsertTools when
//     the whitelist is empty).
//   - clearAll=false with delta: per-signal ops on telemetry.persist; clear
//     ops remove the corresponding key, true/false ops upsert it. After
//     applying, an empty persist mapping is removed; if telemetry is then
//     also empty, the telemetry key itself is removed.
//
// Returns errServerNotFound when the server name is absent.
func patchServerTelemetry(source []byte, serverName string, delta serverPersistDelta, clearAll bool) ([]byte, error) {
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

	if clearAll {
		deleteMappingKey(targetServer, "telemetry")
	} else if delta.hasOps() {
		applyServerPersist(targetServer, delta)
	}
	// All-noop deltas are valid (callers may PATCH with nothing to do); fall
	// through to the round-trip so the response is shaped consistently.

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

func applyServerPersist(server *yaml.Node, delta serverPersistDelta) {
	telemetryNode := findOrCreateMapping(server, "telemetry")
	if telemetryNode == nil {
		return
	}
	persistNode := findOrCreateMapping(telemetryNode, "persist")
	if persistNode == nil {
		return
	}

	applyServerPersistOp(persistNode, "logs", delta.Logs)
	applyServerPersistOp(persistNode, "metrics", delta.Metrics)
	applyServerPersistOp(persistNode, "traces", delta.Traces)

	// Cleanup: collapse empty containers so an all-cleared override doesn't
	// leave behind an empty `telemetry: {persist: {}}` skeleton.
	if len(persistNode.Content) == 0 {
		deleteMappingKey(telemetryNode, "persist")
	}
	if len(telemetryNode.Content) == 0 {
		deleteMappingKey(server, "telemetry")
	}
}

func applyServerPersistOp(persist *yaml.Node, key string, op serverPersistOp) {
	switch op {
	case serverPersistNoop:
	case serverPersistClear:
		deleteMappingKey(persist, key)
	case serverPersistTrue:
		setMappingBool(persist, key, true)
	case serverPersistFalse:
		setMappingBool(persist, key, false)
	}
}

// findOrCreateMapping is the mapping-node analogue of findOrCreateSequence.
// When parent has key with a mapping value it is returned; otherwise a fresh
// empty mapping is installed in place (replacing a non-mapping value or
// appending a new key+mapping pair to the end of parent).
func findOrCreateMapping(parent *yaml.Node, key string) *yaml.Node {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value != key {
			continue
		}
		v := parent.Content[i+1]
		if v.Kind == yaml.MappingNode {
			return v
		}
		// Replacing a non-mapping value (e.g. `telemetry: null` or
		// `telemetry: "oops"`) with an empty mapping. Carry over any
		// comments attached to the old value so the replacement does not
		// silently drop user-written prose. (See pitfall #1 in the spec:
		// "YAML comment loss is silent.")
		m := &yaml.Node{
			Kind:        yaml.MappingNode,
			Tag:         "!!map",
			HeadComment: v.HeadComment,
			LineComment: v.LineComment,
			FootComment: v.FootComment,
		}
		parent.Content[i+1] = m
		return m
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content, keyNode, m)
	return m
}

// deleteMappingKey removes the key/value pair from a mapping node. No-op on
// nil, non-mapping, or absent-key inputs.
func deleteMappingKey(mapping *yaml.Node, key string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}

func setMappingBool(mapping *yaml.Node, key string, val bool) {
	v := "false"
	if val {
		v = "true"
	}
	setScalar(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: v})
}

func setMappingInt(mapping *yaml.Node, key string, val int) {
	setScalar(mapping, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(val)})
}

func setScalar(mapping *yaml.Node, key string, value *yaml.Node) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}
