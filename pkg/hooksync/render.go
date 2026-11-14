package hooksync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gowebpki/jcs"

	"github.com/gridctl/gridctl/pkg/project"
)

const maxHashHistory = 5

// renderedGroup is one matcher group destined for one event.
type renderedGroup struct {
	Event string
	Value map[string]any
}

// hookSpec is the neutral intermediate form of a hook.json: events in
// sorted order, groups in definition order, handlers in definition order.
type hookSpec struct {
	Groups []specGroup
}

type specGroup struct {
	Event      string
	HasMatcher bool
	Matcher    any
	Handlers   []specHandler
}

// specHandler keeps every decoded key in Raw so the identity render
// passes them through unchanged. Script is the handler command relative
// to the hook directory, without a leading "./".
type specHandler struct {
	Script string
	Raw    map[string]any
}

// consumedHandlerKeys are the handler keys a dialect render reads. Every
// other key (statusMessage, async, asyncRewake, onFailure, and any
// passthrough key) is cosmetic and is named when a dialect drops it.
var consumedHandlerKeys = map[string]bool{
	"type":    true,
	"command": true,
	"args":    true,
	"timeout": true,
	"if":      true,
}

// RenderFunc renders one hook for one target. It is a pure function of
// the parsed definition, the hook name, and the script directory.
type RenderFunc func(spec *hookSpec, name, scriptDir string) (rendered, error)

// rendered is one target's render of one hook. A merge-key target fills
// Groups, or Value when it owns one top-level key; a dedicated-file target
// fills Bytes. Unexpressible holds the reason for each group the dialect
// cannot carry, and DroppedGroups the short label for it. A render with no
// Groups, Bytes, or Value writes nothing.
type rendered struct {
	Groups        []renderedGroup
	Bytes         []byte
	Value         any
	DroppedGroups []string
	Unexpressible []string
	DroppedIf     []string
	DroppedKeys   []string
	client        string
}

func (r rendered) expressible() bool {
	return len(r.Groups) > 0 || len(r.Bytes) > 0 || r.Value != nil
}

// detail names what the render dropped. Empty for an identity render.
func (r rendered) detail() string {
	var parts []string
	if len(r.DroppedGroups) > 0 {
		parts = append(parts, "dropped groups: "+strings.Join(r.DroppedGroups, "; "))
	}
	for _, rule := range r.DroppedIf {
		parts = append(parts, fmt.Sprintf("dropped if %q: the handler runs on every matched call", rule))
	}
	if len(r.DroppedKeys) > 0 {
		parts = append(parts, fmt.Sprintf("lossy %s render: dropped handler keys %s", r.client, strings.Join(r.DroppedKeys, ", ")))
	}
	return strings.Join(parts, "; ")
}

func fileHash(data []byte) string {
	sum := sha256.Sum256(data)
	return project.HashScheme + hex.EncodeToString(sum[:])
}

// parseDefinition turns hook.json into the intermediate form.
func parseDefinition(raw []byte, name string) (*hookSpec, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parsing hook definition: %w", err)
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hook %s: hooks must be an object", name)
	}
	events := make([]string, 0, len(hooks))
	for event := range hooks {
		events = append(events, event)
	}
	sort.Strings(events)
	spec := &hookSpec{}
	for _, event := range events {
		arr, ok := hooks[event].([]any)
		if !ok {
			return nil, fmt.Errorf("hook %s: event %q must be an array", name, event)
		}
		for _, item := range arr {
			group, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("hook %s: event %q group must be an object", name, event)
			}
			sg := specGroup{Event: event}
			sg.Matcher, sg.HasMatcher = group["matcher"]
			rawHandlers, ok := group["hooks"].([]any)
			if !ok {
				return nil, fmt.Errorf("hook %s: matcher group is missing hooks", name)
			}
			for _, h := range rawHandlers {
				handler, ok := h.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("hook %s: handler must be an object", name)
				}
				command, _ := handler["command"].(string)
				sg.Handlers = append(sg.Handlers, specHandler{
					Script: strings.TrimPrefix(command, "./"),
					Raw:    handler,
				})
			}
			spec.Groups = append(spec.Groups, sg)
		}
	}
	return spec, nil
}

// scriptPath is the handler's absolute script inside scriptDir.
func (h specHandler) scriptPath(scriptDir string) string {
	return filepath.Join(scriptDir, filepath.FromSlash(h.Script))
}

// args returns the handler's argument list. ParseHookJSON guarantees an
// array of strings when the key is present.
func (h specHandler) args() []string {
	out := []string{}
	items, _ := h.Raw["args"].([]any)
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// timeoutSeconds returns the handler's positive timeout in seconds.
func (h specHandler) timeoutSeconds() (float64, bool) {
	var v float64
	switch t := h.Raw["timeout"].(type) {
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return 0, false
		}
		v = f
	case float64:
		v = t
	default:
		return 0, false
	}
	if v <= 0 {
		return 0, false
	}
	return v, true
}

// cosmeticKeys are the handler keys a dialect drops without changing when
// the handler runs, sorted.
func (h specHandler) cosmeticKeys() []string {
	var keys []string
	for key := range h.Raw {
		if !consumedHandlerKeys[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// identifierAlternation matches a plain "|" alternation of identifier
// characters, the only matcher shape a tool table can map.
var identifierAlternation = regexp.MustCompile(`^[A-Za-z0-9_]+(\|[A-Za-z0-9_]+)*$`)

// matcherTokens parses a Claude matcher. An absent, empty, or "*"
// matcher means every tool and returns all true. Otherwise the string is
// split on "|"; ok is false when it is not a plain alternation.
func matcherTokens(g specGroup) (tokens []string, all, ok bool) {
	if !g.HasMatcher || g.Matcher == nil {
		return nil, true, true
	}
	m, isString := g.Matcher.(string)
	if !isString {
		return nil, false, false
	}
	if m == "" || m == "*" {
		return nil, true, true
	}
	if !identifierAlternation.MatchString(m) {
		return nil, false, false
	}
	return strings.Split(m, "|"), false, true
}

// renderIdentity turns the intermediate form into the groups sync writes
// for Claude Code. args is always present so the client runs the handler
// in exec form. A missing statusMessage becomes "gridctl hook <name>".
func renderIdentity(spec *hookSpec, name, scriptDir string) (rendered, error) {
	groups := make([]renderedGroup, 0, len(spec.Groups))
	for _, g := range spec.Groups {
		out := map[string]any{}
		if g.HasMatcher {
			out["matcher"] = g.Matcher
		}
		handlers := make([]any, 0, len(g.Handlers))
		for _, h := range g.Handlers {
			value := map[string]any{
				"type":    "command",
				"command": h.scriptPath(scriptDir),
			}
			if args, ok := h.Raw["args"]; ok {
				value["args"] = args
			} else {
				value["args"] = []any{}
			}
			for key, v := range h.Raw {
				if key == "type" || key == "command" || key == "args" {
					continue
				}
				value[key] = v
			}
			if _, ok := value["statusMessage"]; !ok {
				value["statusMessage"] = "gridctl hook " + name
			}
			handlers = append(handlers, value)
		}
		out["hooks"] = handlers
		groups = append(groups, renderedGroup{Event: g.Event, Value: out})
	}
	return rendered{Groups: groups}, nil
}

// renderFor parses raw and renders it for t.
func renderFor(t Target, raw []byte, name, scriptDir string) (rendered, error) {
	spec, err := parseDefinition(raw, name)
	if err != nil {
		return rendered{}, err
	}
	render := t.Render
	if render == nil {
		render = renderIdentity
	}
	return render(spec, name, scriptDir)
}

// appendUnique appends s to list when it is not already present.
func appendUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

// groupsHash is the wiring ValueHash rule (RFC 8785) over the rendered
// group list, sorted by event then canonical JSON.
func groupsHash(groups []renderedGroup) (string, error) {
	type item struct {
		Event string         `json:"event"`
		Group map[string]any `json:"group"`
	}
	items := make([]item, len(groups))
	for i, g := range groups {
		items[i] = item{Event: g.Event, Group: g.Value}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Event != items[j].Event {
			return items[i].Event < items[j].Event
		}
		return canonicalText(items[i].Group) < canonicalText(items[j].Group)
	})
	return valueHash(items)
}

func canonicalText(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	canonical, err := jcs.Transform(data)
	if err != nil {
		return string(data)
	}
	return string(canonical)
}

func valueHash(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("encoding hook groups: %w", err)
	}
	canonical, err := jcs.Transform(data)
	if err != nil {
		return "", fmt.Errorf("canonicalizing hook groups: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return project.HashScheme + hex.EncodeToString(sum[:]), nil
}

func hashRecorded(hashes []string, hash string) bool {
	for _, h := range hashes {
		if h == hash {
			return true
		}
	}
	return false
}

func appendHash(hashes []string, hash string) []string {
	kept := make([]string, 0, len(hashes)+1)
	for _, h := range hashes {
		if h != hash {
			kept = append(kept, h)
		}
	}
	kept = append(kept, hash)
	if len(kept) > maxHashHistory {
		kept = kept[len(kept)-maxHashHistory:]
	}
	return kept
}
