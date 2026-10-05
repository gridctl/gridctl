package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Known object keys are typed fields. Anything else is retained in Extra
// and merged back on encode so a tools/list or tools/call round trip does
// not drop keys the gateway does not interpret.

var contentKnownKeys = map[string]struct{}{
	"type": {}, "text": {}, "data": {}, "mimeType": {}, "uri": {},
	"name": {}, "title": {}, "description": {}, "size": {},
	"resource": {}, "annotations": {}, "_meta": {},
}

var toolKnownKeys = map[string]struct{}{
	"name": {}, "title": {}, "description": {}, "inputSchema": {},
	"outputSchema": {}, "annotations": {}, "icons": {}, "execution": {},
	"_meta": {},
}

var annotationKnownKeys = map[string]struct{}{
	"title": {}, "readOnlyHint": {}, "destructiveHint": {},
	"idempotentHint": {}, "openWorldHint": {},
}

// contentWire field order matches the historical Content struct so a
// text-only block stays byte-identical. New fields follow, omitted when
// empty. Extra is merged after this object.
type contentWire struct {
	Type        string          `json:"type"`
	Text        string          `json:"text,omitempty"`
	Data        string          `json:"data,omitempty"`
	MimeType    string          `json:"mimeType,omitempty"`
	URI         string          `json:"uri,omitempty"`
	Name        string          `json:"name,omitempty"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Size        *int64          `json:"size,omitempty"`
	Resource    json.RawMessage `json:"resource,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

type toolWire struct {
	Name         string           `json:"name"`
	Title        string           `json:"title,omitempty"`
	Description  string           `json:"description,omitempty"`
	InputSchema  json.RawMessage  `json:"inputSchema"`
	OutputSchema json.RawMessage  `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations `json:"annotations,omitempty"`
	Icons        json.RawMessage  `json:"icons,omitempty"`
	Execution    json.RawMessage  `json:"execution,omitempty"`
	Meta         json.RawMessage  `json:"_meta,omitempty"`
}

type annotationWire struct {
	Title           string `json:"title,omitempty"`
	ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// MarshalJSON is a value receiver so []Content elements encode through it.
func (c Content) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(contentWire{
		Type:        c.Type,
		Text:        c.Text,
		Data:        c.Data,
		MimeType:    c.MimeType,
		URI:         c.URI,
		Name:        c.Name,
		Title:       c.Title,
		Description: c.Description,
		Size:        c.Size,
		Resource:    omitRaw(c.Resource),
		Annotations: omitRaw(c.Annotations),
		Meta:        omitRaw(c.Meta),
	})
	if err != nil {
		return nil, err
	}
	return appendRawKeys(body, c.Extra, contentKnownKeys)
}

// UnmarshalJSON decodes into a map first. Unmarshaling into Content here
// would recurse through this method.
func (c *Content) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		*c = Content{}
		return nil
	}
	raw, err := decodeObject(data)
	if err != nil {
		return err
	}
	next := Content{}
	if next.Type, err = takeString(raw, "type"); err != nil {
		return err
	}
	if next.Text, err = takeString(raw, "text"); err != nil {
		return err
	}
	if next.Data, err = takeString(raw, "data"); err != nil {
		return err
	}
	if next.MimeType, err = takeString(raw, "mimeType"); err != nil {
		return err
	}
	if next.URI, err = takeString(raw, "uri"); err != nil {
		return err
	}
	if next.Name, err = takeString(raw, "name"); err != nil {
		return err
	}
	if next.Title, err = takeString(raw, "title"); err != nil {
		return err
	}
	if next.Description, err = takeString(raw, "description"); err != nil {
		return err
	}
	if next.Size, err = takeInt64Ptr(raw, "size"); err != nil {
		return err
	}
	next.Resource = takeRaw(raw, "resource")
	next.Annotations = takeRaw(raw, "annotations")
	next.Meta = takeRaw(raw, "_meta")
	next.Extra = takeExtra(raw, contentKnownKeys)
	*c = next
	return nil
}

func (t Tool) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(toolWire{
		Name:         t.Name,
		Title:        t.Title,
		Description:  t.Description,
		InputSchema:  t.InputSchema,
		OutputSchema: omitRaw(t.OutputSchema),
		Annotations:  t.Annotations,
		Icons:        omitRaw(t.Icons),
		Execution:    omitRaw(t.Execution),
		Meta:         omitRaw(t.Meta),
	})
	if err != nil {
		return nil, err
	}
	return appendRawKeys(body, t.Extra, toolKnownKeys)
}

func (t *Tool) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		*t = Tool{}
		return nil
	}
	raw, err := decodeObject(data)
	if err != nil {
		return err
	}
	next := Tool{}
	if next.Name, err = takeString(raw, "name"); err != nil {
		return err
	}
	if next.Title, err = takeString(raw, "title"); err != nil {
		return err
	}
	if next.Description, err = takeString(raw, "description"); err != nil {
		return err
	}
	next.InputSchema = takeRaw(raw, "inputSchema")
	next.OutputSchema = takeRaw(raw, "outputSchema")
	if next.Annotations, err = takeAnnotations(raw); err != nil {
		return err
	}
	next.Icons = takeRaw(raw, "icons")
	next.Execution = takeRaw(raw, "execution")
	next.Meta = takeRaw(raw, "_meta")
	next.Extra = takeExtra(raw, toolKnownKeys)
	*t = next
	return nil
}

func (a ToolAnnotations) MarshalJSON() ([]byte, error) {
	body, err := json.Marshal(annotationWire{
		Title:           a.Title,
		ReadOnlyHint:    a.ReadOnlyHint,
		DestructiveHint: a.DestructiveHint,
		IdempotentHint:  a.IdempotentHint,
		OpenWorldHint:   a.OpenWorldHint,
	})
	if err != nil {
		return nil, err
	}
	return appendRawKeys(body, a.Extra, annotationKnownKeys)
}

func (a *ToolAnnotations) UnmarshalJSON(data []byte) error {
	if isJSONNull(data) {
		*a = ToolAnnotations{}
		return nil
	}
	raw, err := decodeObject(data)
	if err != nil {
		return err
	}
	next := ToolAnnotations{}
	if next.Title, err = takeString(raw, "title"); err != nil {
		return err
	}
	if next.ReadOnlyHint, err = takeBoolPtr(raw, "readOnlyHint"); err != nil {
		return err
	}
	if next.DestructiveHint, err = takeBoolPtr(raw, "destructiveHint"); err != nil {
		return err
	}
	if next.IdempotentHint, err = takeBoolPtr(raw, "idempotentHint"); err != nil {
		return err
	}
	if next.OpenWorldHint, err = takeBoolPtr(raw, "openWorldHint"); err != nil {
		return err
	}
	next.Extra = takeExtra(raw, annotationKnownKeys)
	*a = next
	return nil
}

func contentBlockSize(c Content) (int, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}

func isJSONNull(raw []byte) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// omitRaw turns JSON null into nil so omitempty drops it. A RawMessage
// holding the literal null is not empty to encoding/json.
func omitRaw(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || isJSONNull(raw) {
		return nil
	}
	return raw
}

func decodeObject(data []byte) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]json.RawMessage{}
	}
	return raw, nil
}

func takeString(raw map[string]json.RawMessage, key string) (string, error) {
	v, ok := raw[key]
	if !ok || isJSONNull(v) {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(v, &s); err != nil {
		return "", fmt.Errorf("field %q: %w", key, err)
	}
	return s, nil
}

func takeRaw(raw map[string]json.RawMessage, key string) json.RawMessage {
	v, ok := raw[key]
	if !ok || isJSONNull(v) {
		return nil
	}
	return append(json.RawMessage(nil), v...)
}

func takeInt64Ptr(raw map[string]json.RawMessage, key string) (*int64, error) {
	v, ok := raw[key]
	if !ok || isJSONNull(v) {
		return nil, nil
	}
	var n int64
	if err := json.Unmarshal(v, &n); err != nil {
		return nil, fmt.Errorf("field %q: %w", key, err)
	}
	return &n, nil
}

func takeBoolPtr(raw map[string]json.RawMessage, key string) (*bool, error) {
	v, ok := raw[key]
	if !ok || isJSONNull(v) {
		return nil, nil
	}
	var b bool
	if err := json.Unmarshal(v, &b); err != nil {
		return nil, fmt.Errorf("field %q: %w", key, err)
	}
	return &b, nil
}

func takeAnnotations(raw map[string]json.RawMessage) (*ToolAnnotations, error) {
	v, ok := raw["annotations"]
	if !ok || isJSONNull(v) {
		return nil, nil
	}
	var ann ToolAnnotations
	if err := json.Unmarshal(v, &ann); err != nil {
		return nil, fmt.Errorf("field %q: %w", "annotations", err)
	}
	return &ann, nil
}

func takeExtra(raw map[string]json.RawMessage, known map[string]struct{}) map[string]json.RawMessage {
	var extra map[string]json.RawMessage
	for k, v := range raw {
		if _, ok := known[k]; ok {
			continue
		}
		if extra == nil {
			extra = make(map[string]json.RawMessage, 1)
		}
		extra[k] = append(json.RawMessage(nil), v...)
	}
	return extra
}

func appendRawKeys(body []byte, extra map[string]json.RawMessage, known map[string]struct{}) ([]byte, error) {
	if len(extra) == 0 {
		return body, nil
	}
	keys := make([]string, 0, len(extra))
	for k, v := range extra {
		if _, ok := known[k]; ok {
			continue
		}
		if len(v) == 0 || !json.Valid(v) {
			return nil, fmt.Errorf("field %q: invalid JSON", k)
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return body, nil
	}
	if len(body) < 2 || body[0] != '{' || body[len(body)-1] != '}' {
		return nil, fmt.Errorf("opaque object is not a JSON object")
	}
	sort.Strings(keys)
	out := make([]byte, 0, len(body)+len(keys)*16)
	out = append(out, body[:len(body)-1]...)
	if len(body) > 2 {
		out = append(out, ',')
	}
	for i, k := range keys {
		if i > 0 {
			out = append(out, ',')
		}
		key, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		out = append(out, key...)
		out = append(out, ':')
		out = append(out, extra[k]...)
	}
	out = append(out, '}')
	return out, nil
}

func cloneBool(b *bool) *bool {
	if b == nil {
		return nil
	}
	v := *b
	return &v
}

func cloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}
