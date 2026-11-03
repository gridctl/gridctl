package hooksync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
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

// renderDefinition turns hook.json into the groups sync writes. args is
// always present so the client runs the handler in exec form. A missing
// statusMessage becomes "gridctl hook <name>".
func renderDefinition(raw []byte, name, scriptDir string) ([]renderedGroup, error) {
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
	var groups []renderedGroup
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
			rendered, err := renderGroup(name, scriptDir, group)
			if err != nil {
				return nil, err
			}
			groups = append(groups, renderedGroup{Event: event, Value: rendered})
		}
	}
	return groups, nil
}

func renderGroup(name, scriptDir string, group map[string]any) (map[string]any, error) {
	out := map[string]any{}
	if matcher, ok := group["matcher"]; ok {
		out["matcher"] = matcher
	}
	rawHandlers, ok := group["hooks"].([]any)
	if !ok {
		return nil, fmt.Errorf("hook %s: matcher group is missing hooks", name)
	}
	handlers := make([]any, 0, len(rawHandlers))
	for _, item := range rawHandlers {
		handler, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("hook %s: handler must be an object", name)
		}
		command, _ := handler["command"].(string)
		rel := strings.TrimPrefix(command, "./")
		rendered := map[string]any{
			"type":    "command",
			"command": filepath.Join(scriptDir, filepath.FromSlash(rel)),
		}
		if args, ok := handler["args"]; ok {
			rendered["args"] = args
		} else {
			rendered["args"] = []any{}
		}
		for key, value := range handler {
			if key == "type" || key == "command" || key == "args" {
				continue
			}
			rendered[key] = value
		}
		if _, ok := rendered["statusMessage"]; !ok {
			rendered["statusMessage"] = "gridctl hook " + name
		}
		handlers = append(handlers, rendered)
	}
	out["hooks"] = handlers
	return out, nil
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
