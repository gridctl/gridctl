package a2aclient

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// Operation is the bounded, non-streaming subset of A2A JSON-RPC.
type Operation uint8

const (
	Send Operation = iota + 1
	Get
	Cancel
)

// Request contains protocol IDs supplied by trusted routing, never capabilities.
type Request struct {
	ID, MessageID, TaskID, ContextID string
	Text                             string
	Data                             map[string]any
	SkillID                          string
	OutputModes                      []string
	ReturnImmediately                bool
	HistoryLength                    int
}

// Part holds accepted application content. Numbers in Data retain JSON precision.
type Part struct {
	Type string         `json:"type"`
	Text string         `json:"text,omitempty"`
	Data map[string]any `json:"data,omitempty"`
}

// MarshalJSON preserves empty text and empty data objects as application content.
func (p Part) MarshalJSON() ([]byte, error) {
	var value any
	switch p.Type {
	case "text":
		value = p.Text
	case "data":
		if p.Data == nil {
			return nil, codecError("invalid_data_part")
		}
		value = p.Data
	default:
		return nil, codecError("invalid_part")
	}
	body, err := json.Marshal(map[string]any{"type": p.Type, p.Type: value})
	if err != nil {
		return nil, codecError("invalid_part")
	}
	return body, nil
}

// Message preserves boundaries and validated protocol references for admission.
type Message struct {
	TaskID, ContextID string
	Role              string
	Parts             []Part
}

// Artifact preserves an artifact's application fields without its protocol ID.
type Artifact struct {
	Name, Description string
	Parts             []Part
}

// Result is decoded evidence, not authority. Routing must validate its top-level
// IDs against the admitted operation before issuing or updating capabilities.
type Result struct {
	Kind, TaskID, ContextID, State string
	Parts                          []Part
	StatusMessages, History        []Message
	Artifacts                      []Artifact
}

func codecError(category string) error { return &Error{Category: category} }

func validID(id string, required bool) bool {
	return (!required || id != "") && len(id) <= 1024 && utf8.ValidString(id)
}

func method(version string, op Operation) string {
	if version == "1.0" {
		switch op {
		case Send:
			return "SendMessage"
		case Get:
			return "GetTask"
		case Cancel:
			return "CancelTask"
		}
	}
	if version == "0.3" {
		switch op {
		case Send:
			return "message/send"
		case Get:
			return "tasks/get"
		case Cancel:
			return "tasks/cancel"
		}
	}
	return ""
}

// EncodeRequest encodes only the supported fields, bounding aggregate input.
func EncodeRequest(version string, op Operation, r Request) ([]byte, error) {
	m := method(version, op)
	if m == "" || !validID(r.ID, true) || !validID(r.TaskID, op != Send) || !validID(r.ContextID, false) {
		return nil, codecError("invalid_request")
	}
	params := map[string]any{"id": r.TaskID}
	if op == Get {
		if r.HistoryLength < 0 || r.HistoryLength > 100 {
			return nil, codecError("invalid_history_length")
		}
		params["historyLength"] = r.HistoryLength
	}
	if op == Send {
		if !validID(r.MessageID, true) || !utf8.ValidString(r.Text) || r.TaskID != "" && r.ContextID == "" {
			return nil, codecError("invalid_message")
		}
		parts := []map[string]any{{"text": r.Text}}
		size := len(r.Text)
		if r.Data != nil {
			data, err := json.Marshal(r.Data)
			if err != nil {
				return nil, codecError("invalid_data")
			}
			size += len(data)
			parts = append(parts, map[string]any{"data": r.Data})
		}
		if size > 256<<10 {
			return nil, codecError("input_too_large")
		}
		role := "ROLE_USER"
		configuration := map[string]any{"returnImmediately": r.ReturnImmediately, "acceptedOutputModes": r.OutputModes}
		if version == "0.3" {
			role = "user"
			configuration = map[string]any{"blocking": !r.ReturnImmediately, "acceptedOutputModes": r.OutputModes}
			for _, part := range parts {
				if _, ok := part["text"]; ok {
					part["kind"] = "text"
				} else {
					part["kind"] = "data"
				}
			}
		}
		message := map[string]any{"messageId": r.MessageID, "role": role, "parts": parts}
		if version == "0.3" {
			message["kind"] = "message"
		}
		if r.ContextID != "" {
			message["contextId"] = r.ContextID
		}
		if r.TaskID != "" {
			message["taskId"] = r.TaskID
		}
		if r.SkillID != "" {
			message["metadata"] = map[string]string{"skill_id": r.SkillID}
		}
		params = map[string]any{"message": message, "configuration": configuration}
	}
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": r.ID, "method": m, "params": params})
	if err != nil {
		return nil, codecError("invalid_request")
	}
	return encoded, nil
}

// decodeJSON refuses duplicate members and trailing documents, including within
// application data. Ambiguous objects must not select different routing IDs in
// different protocol implementations.
func decodeJSON(body []byte, target any) error {
	if !utf8.Valid(body) {
		return codecError("invalid_json")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if err := checkValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return codecError("invalid_json")
	}
	d = json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if d.Decode(target) != nil {
		return codecError("invalid_json")
	}
	return nil
}

func checkValue(d *json.Decoder, depth int) error {
	if depth > 128 {
		return codecError("invalid_json")
	}
	t, err := d.Token()
	if err != nil {
		return codecError("invalid_json")
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	keys := make(map[string]bool)
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return codecError("invalid_json")
			}
			s, ok := key.(string)
			if !ok || keys[s] {
				return codecError("invalid_json")
			}
			keys[s] = true
		}
		if err := checkValue(d, depth+1); err != nil {
			return err
		}
	}
	if _, err := d.Token(); err != nil {
		return codecError("invalid_json")
	}
	return nil
}

type wireMessage struct {
	Kind       string            `json:"kind"`
	MessageID  string            `json:"messageId"`
	TaskID     string            `json:"taskId"`
	ContextID  string            `json:"contextId"`
	Role       string            `json:"role"`
	Parts      []json.RawMessage `json:"parts"`
	References []string          `json:"referenceTaskIds"`
}

type wireTask struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	ContextID string `json:"contextId"`
	Status    struct {
		State   string       `json:"state"`
		Message *wireMessage `json:"message"`
	} `json:"status"`
	History   []wireMessage `json:"history"`
	Artifacts []struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Parts       []json.RawMessage `json:"parts"`
	} `json:"artifacts"`
}

// DecodeResponse validates correlation, variants, and nested protocol references.
// JSON-RPC errors are decoded on every HTTP status without retaining remote text.
func DecodeResponse(version string, op Operation, id string, status int, body []byte) (Result, error) {
	if method(version, op) == "" || !validID(id, true) {
		return Result{}, codecError("invalid_request")
	}
	if len(body) > maxRPCBytes {
		return Result{}, codecError("response_too_large")
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if err := decodeJSON(body, &envelope); err != nil {
		if status < 200 || status >= 300 {
			return Result{}, &Error{Category: "http_failed", Status: status, Retryable: status == 429}
		}
		return Result{}, codecError("invalid_rpc_response")
	}
	if envelope.JSONRPC != "2.0" || envelope.ID != id || (len(envelope.Result) == 0) == (len(envelope.Error) == 0) {
		if envelope.JSONRPC == "" && (status < 200 || status >= 300) {
			return Result{}, &Error{Category: "http_failed", Status: status, Retryable: status == 429}
		}
		return Result{}, codecError("invalid_rpc_response")
	}
	if len(envelope.Error) != 0 {
		return Result{}, decodeRPCError(status, envelope.Error)
	}
	return decodeResult(version, op, envelope.Result)
}

func decodeRPCError(status int, body []byte) error {
	var remote struct {
		Code    *int    `json:"code"`
		Message *string `json:"message"`
	}
	if json.Unmarshal(body, &remote) != nil || remote.Code == nil || remote.Message == nil {
		return codecError("invalid_rpc_error")
	}
	return &Error{Category: "rpc_failed", Status: status, Code: *remote.Code,
		Retryable: *remote.Code == -32053 || (*remote.Code == -32054 && *remote.Message == "Session operation in progress, please retry")}
}

func decodeResult(version string, op Operation, body []byte) (Result, error) {
	kind := "task"
	if version == "1.0" && op == Send {
		var wrapper map[string]json.RawMessage
		if json.Unmarshal(body, &wrapper) != nil || len(wrapper) != 1 {
			return Result{}, codecError("invalid_result_variant")
		}
		if v, ok := wrapper["message"]; ok {
			kind, body = "message", v
		} else if v, ok := wrapper["task"]; ok {
			body = v
		} else {
			return Result{}, codecError("invalid_result_variant")
		}
	} else if version == "0.3" {
		var discriminator struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(body, &discriminator) != nil || (discriminator.Kind != "task" && discriminator.Kind != "message") {
			return Result{}, codecError("invalid_result_variant")
		}
		kind = discriminator.Kind
	}
	if kind == "message" {
		if op != Send {
			return Result{}, codecError("invalid_result_variant")
		}
		var wire wireMessage
		if json.Unmarshal(body, &wire) != nil {
			return Result{}, codecError("invalid_message")
		}
		message, err := decodeMessage(version, wire, wire.TaskID, wire.ContextID)
		if err != nil {
			return Result{}, err
		}
		return Result{Kind: kind, TaskID: message.TaskID, ContextID: message.ContextID, Parts: message.Parts}, nil
	}
	var wire wireTask
	if json.Unmarshal(body, &wire) != nil || !validID(wire.ID, true) || !validID(wire.ContextID, false) || version == "1.0" && wire.Kind != "" {
		return Result{}, codecError("invalid_task")
	}
	state := wire.Status.State
	if version == "1.0" {
		if !strings.HasPrefix(state, "TASK_STATE_") || state != strings.ToUpper(state) {
			return Result{}, codecError("invalid_task_state")
		}
		state = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(state, "TASK_STATE_")), "_", "-")
	}
	switch state {
	case "submitted", "working", "input-required", "auth-required", "completed", "failed", "canceled", "rejected":
	default:
		return Result{}, codecError("invalid_task_state")
	}
	r := Result{Kind: "task", TaskID: wire.ID, ContextID: wire.ContextID, State: state}
	if wire.Status.Message != nil {
		m, err := decodeMessage(version, *wire.Status.Message, wire.ID, wire.ContextID)
		if err != nil {
			return Result{}, err
		}
		r.StatusMessages = []Message{m}
	}
	for _, entry := range wire.History {
		m, err := decodeMessage(version, entry, wire.ID, wire.ContextID)
		if err != nil {
			return Result{}, err
		}
		r.History = append(r.History, m)
	}
	for _, entry := range wire.Artifacts {
		parts, err := decodeParts(version, entry.Parts)
		if err != nil {
			return Result{}, err
		}
		r.Artifacts = append(r.Artifacts, Artifact{Name: entry.Name, Description: entry.Description, Parts: parts})
	}
	return r, nil
}

func decodeMessage(version string, wire wireMessage, taskID, contextID string) (Message, error) {
	if !validID(wire.MessageID, true) || !validID(wire.TaskID, false) || !validID(wire.ContextID, false) ||
		wire.TaskID != "" && wire.TaskID != taskID || wire.ContextID != "" && wire.ContextID != contextID {
		return Message{}, codecError("protocol_conflict")
	}
	for _, ref := range wire.References {
		if ref == "" || ref != taskID {
			return Message{}, codecError("protocol_conflict")
		}
	}
	role := wire.Role
	if version == "1.0" {
		if wire.Kind != "" {
			return Message{}, codecError("invalid_message")
		}
		switch role {
		case "ROLE_USER":
			role = "user"
		case "ROLE_AGENT":
			role = "agent"
		default:
			return Message{}, codecError("invalid_message_role")
		}
	} else if wire.Kind != "message" {
		return Message{}, codecError("invalid_message")
	}
	if role != "user" && role != "agent" {
		return Message{}, codecError("invalid_message_role")
	}
	parts, err := decodeParts(version, wire.Parts)
	if err != nil {
		return Message{}, err
	}
	return Message{TaskID: wire.TaskID, ContextID: wire.ContextID, Role: role, Parts: parts}, nil
}

func decodeParts(version string, raw []json.RawMessage) ([]Part, error) {
	if len(raw) == 0 {
		return nil, codecError("invalid_parts")
	}
	parts := make([]Part, 0, len(raw))
	for _, body := range raw {
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil || fields == nil {
			return nil, codecError("invalid_part")
		}
		for _, unsupported := range []string{"url", "raw", "file"} {
			if _, ok := fields[unsupported]; ok {
				return nil, codecError("unsupported_part_" + unsupported)
			}
		}
		_, text := fields["text"]
		_, data := fields["data"]
		if text == data {
			return nil, codecError("invalid_part")
		}
		kind := "text"
		if data {
			kind = "data"
		}
		if version == "0.3" {
			var discriminator string
			if json.Unmarshal(fields["kind"], &discriminator) != nil || discriminator != kind {
				return nil, codecError("unsupported_part_kind")
			}
		} else if _, ok := fields["kind"]; ok {
			return nil, codecError("invalid_part")
		}
		p := Part{Type: kind}
		if text {
			if bytes.Equal(fields["text"], []byte("null")) || json.Unmarshal(fields["text"], &p.Text) != nil {
				return nil, codecError("invalid_text_part")
			}
		} else if decodeJSON(fields["data"], &p.Data) != nil || p.Data == nil {
			return nil, codecError("invalid_data_part")
		}
		parts = append(parts, p)
	}
	return parts, nil
}
