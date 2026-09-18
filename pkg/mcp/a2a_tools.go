package mcp

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"unicode/utf8"

	"github.com/gridctl/gridctl/pkg/a2aclient"
	"github.com/gridctl/gridctl/pkg/logging"
)

const a2aSkillAdvisory = "\n\nAdvisory conversational entry point. The remote agent is not restricted to this skill."

var a2aSkillCharacters = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
var a2aToolCharacters = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type a2aToolSet struct {
	tools   []Tool
	agent   a2aclient.Compatibility
	skills  map[string]a2aclient.Compatibility
	byTool  map[string]string
	omitted map[string]string
}

func a2aLocalError(category string) error { return &a2aclient.Error{Category: category} }

func buildA2ATools(server string, cfg A2AClientConfig, card *a2aclient.Card, version string) (a2aToolSet, error) {
	set := a2aToolSet{
		skills: make(map[string]a2aclient.Compatibility),
		byTool: make(map[string]string), omitted: make(map[string]string),
	}
	var err error
	set.agent, err = a2aclient.CheckCompatibility(card, nil, version, cfg.Token != "", cfg.Profile == "bedrock")
	if err != nil {
		return set, err
	}
	selected := make(map[string]bool, len(cfg.Include))
	for _, id := range cfg.Include {
		selected[id] = true
	}
	validName := func(name string) bool {
		return a2aToolCharacters.MatchString(server+"__"+name) && len(server)+2+len(name) <= 64
	}
	for _, name := range []string{"send", "task_get", "task_cancel"} {
		if !validName(name) {
			return set, a2aLocalError("invalid_tool_name")
		}
	}
	set.tools = []Tool{
		{Name: "send", Description: "Send a conversational message to the remote agent.", InputSchema: a2aMessageSchema(true)},
		{Name: "task_get", Description: "Read a remote task using its bearer task capability.", InputSchema: json.RawMessage(`{"type":"object","properties":{"task_handle":{"type":"string","minLength":1},"history_length":{"type":"integer","minimum":0,"maximum":100,"default":0}},"required":["task_handle"],"additionalProperties":false}`)},
		{Name: "task_cancel", Description: "Request remote cancellation using a bearer task capability.", InputSchema: json.RawMessage(`{"type":"object","properties":{"task_handle":{"type":"string","minLength":1}},"required":["task_handle"],"additionalProperties":false}`)},
	}
	ids, names := make(map[string]bool), make(map[string]bool)
	for i := range card.Skills {
		skill := &card.Skills[i]
		name := "skill-" + a2aSkillCharacters.ReplaceAllString(skill.ID, "-")
		if skill.ID == "" || ids[skill.ID] || name == "skill-" || names[name] || !validName(name) {
			return set, a2aLocalError("invalid_or_colliding_skill_name")
		}
		ids[skill.ID], names[name] = true, true
		compatibility, err := a2aclient.CheckCompatibility(card, skill, version, cfg.Token != "", cfg.Profile == "bedrock")
		if err != nil {
			// Only fixed compatibility categories and sanitized names reach status.
			set.omitted[logging.RedactString(name)] = err.Error()
			if selected[skill.ID] {
				return set, fmt.Errorf("a2a: included skill %s is incompatible: %w", logging.RedactString(name), err)
			}
			continue
		}
		if len(selected) != 0 && !selected[skill.ID] {
			continue
		}
		set.skills[skill.ID], set.byTool[name] = compatibility, skill.ID
		set.tools = append(set.tools, Tool{Name: name, Description: skill.Description + a2aSkillAdvisory, InputSchema: a2aMessageSchema(false)})
	}
	for id := range selected {
		if !ids[id] {
			return set, a2aLocalError("included_skill_missing")
		}
	}
	return set, nil
}

func a2aMessageSchema(generic bool) json.RawMessage {
	skill := ""
	if generic {
		skill = `,"skill_id":{"type":"string","minLength":1}`
	}
	return json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"},"data":{"type":"object"},"context_handle":{"type":"string","minLength":1},"task_handle":{"type":"string","minLength":1},"return_immediately":{"type":"boolean","default":false}` + skill + `},"required":["message"],"additionalProperties":false}`)
}

type a2aArguments struct {
	operation                 capabilityOperationKind
	contextHandle, taskHandle string
	request                   a2aclient.Request
}

func (set a2aToolSet) arguments(tool string, args map[string]any) (a2aArguments, error) {
	parsed := a2aArguments{operation: capabilitySend}
	allowed := map[string]bool{"message": true, "data": true, "context_handle": true, "task_handle": true, "return_immediately": true}
	compatibility := set.agent
	switch tool {
	case "task_get":
		parsed.operation = capabilityGet
		allowed = map[string]bool{"task_handle": true, "history_length": true}
	case "task_cancel":
		parsed.operation = capabilityCancel
		allowed = map[string]bool{"task_handle": true}
	case "send":
		allowed["skill_id"] = true
	default:
		id, ok := set.byTool[tool]
		if !ok {
			return parsed, a2aLocalError("unknown_tool")
		}
		parsed.request.SkillID, compatibility = id, set.skills[id]
	}
	for key := range args {
		if !allowed[key] {
			return parsed, a2aLocalError("invalid_arguments")
		}
	}
	for key, target := range map[string]*string{"context_handle": &parsed.contextHandle, "task_handle": &parsed.taskHandle} {
		if value, present := args[key]; present {
			text, ok := value.(string)
			if !ok || text == "" {
				return parsed, errCapabilityUnavailable
			}
			*target = text
		}
	}
	if parsed.operation != capabilitySend {
		if parsed.taskHandle == "" {
			return parsed, errCapabilityUnavailable
		}
		if value, present := args["history_length"]; present {
			n, ok := a2aHistoryLength(value)
			if !ok {
				return parsed, a2aLocalError("invalid_history_length")
			}
			parsed.request.HistoryLength = n
		}
		return parsed, nil
	}
	if parsed.taskHandle != "" && parsed.contextHandle == "" {
		return parsed, errCapabilityUnavailable
	}
	if value, present := args["skill_id"]; present {
		id, ok := value.(string)
		if !ok || id == "" {
			return parsed, a2aLocalError("invalid_skill")
		}
		compatibility, ok = set.skills[id]
		if !ok {
			return parsed, a2aLocalError("unavailable_skill")
		}
		parsed.request.SkillID = id
	}
	message, ok := args["message"].(string)
	if !ok || !utf8.ValidString(message) {
		return parsed, a2aLocalError("invalid_message")
	}
	parsed.request.Text = message
	size := len(message)
	if data, present := args["data"]; present {
		object, ok := data.(map[string]any)
		if !ok || object == nil || !compatibility.AcceptsData {
			return parsed, a2aLocalError("invalid_data")
		}
		encoded, err := json.Marshal(object)
		if err != nil {
			return parsed, a2aLocalError("invalid_data")
		}
		size += len(encoded)
		parsed.request.Data = object
	}
	if size > 256<<10 {
		return parsed, a2aLocalError("input_too_large")
	}
	if value, present := args["return_immediately"]; present {
		immediate, ok := value.(bool)
		if !ok {
			return parsed, a2aLocalError("invalid_return_immediately")
		}
		parsed.request.ReturnImmediately = immediate
	}
	parsed.request.OutputModes = append([]string(nil), compatibility.OutputModes...)
	return parsed, nil
}

func a2aHistoryLength(value any) (int, bool) {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case int:
		n = float64(v)
	case json.Number:
		var err error
		n, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return int(n), n >= 0 && n <= 100 && math.Trunc(n) == n
}
