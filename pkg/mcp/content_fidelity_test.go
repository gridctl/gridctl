package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

)

const mixedContentWire = `{"content":[{"type":"text","text":"Screenshot taken","annotations":{"audience":["user"],"priority":0.9},"_meta":{"k":"v"}},{"type":"image","data":"iVBORw0K","mimeType":"image/png"},{"type":"audio","data":"UklGRg==","mimeType":"audio/wav"},{"type":"resource_link","uri":"file:///a.rs","name":"a.rs","mimeType":"text/x-rust","size":42},{"type":"resource","resource":{"uri":"ui://excalidraw/canvas","mimeType":"text/html","text":"<html></html>"}},{"type":"resource","resource":{"uri":"file:///b.bin","mimeType":"application/octet-stream","blob":"AAEC"}},{"type":"future","x":1}],"_meta":{"result-level":true}}`

const toolDefinitionWire = `{"name":"render","title":"Render","description":"draws","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true,"x-custom":"keep"},"icons":[{"src":"icon.png","mimeType":"image/png"}],"execution":{"taskSupport":"optional"},"_meta":{"ui":{"resourceUri":"ui://app/index.html"}},"futureField":1}`

func TestToolCallResult_MixedContentRoundTrip(t *testing.T) {
	var result ToolCallResult
	if err := json.Unmarshal([]byte(mixedContentWire), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.Content) != 7 {
		t.Fatalf("got %d blocks", len(result.Content))
	}
	text := result.Content[0]
	if text.Type != "text" || text.Text != "Screenshot taken" {
		t.Fatalf("text block: %+v", text)
	}
	if text.Extra != nil {
		t.Fatalf("known keys leaked into Extra: %+v", text.Extra)
	}
	image := result.Content[1]
	if image.Data != "iVBORw0K" || image.MimeType != "image/png" {
		t.Fatalf("image block lost payload: %+v", image)
	}
	link := result.Content[3]
	if link.Size == nil || *link.Size != 42 || link.URI != "file:///a.rs" || link.Name != "a.rs" {
		t.Fatalf("resource_link: %+v", link)
	}
	future := result.Content[6]
	if future.Type != "future" || string(future.Extra["x"]) != "1" {
		t.Fatalf("unknown block: %+v extra=%v", future, future.Extra)
	}
	if _, ok := result.Meta["result-level"]; !ok {
		t.Fatal("result-level _meta dropped")
	}

	out, err := json.Marshal(&result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	assertJSONEqual(t, out, []byte(mixedContentWire))
}

func TestContent_SizeZeroAndNullRawOmitted(t *testing.T) {
	var block Content
	if err := json.Unmarshal([]byte(`{"type":"resource_link","uri":"file:///a","name":"a","size":0,"annotations":null,"_meta":null}`), &block); err != nil {
		t.Fatal(err)
	}
	if block.Size == nil || *block.Size != 0 {
		t.Fatalf("size 0 must be present, got %+v", block.Size)
	}
	if block.Annotations != nil || block.Meta != nil {
		t.Fatal("null raw fields must become nil so re-encoding omits them")
	}
	out, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, out, []byte(`{"type":"resource_link","uri":"file:///a","name":"a","size":0}`))
}

func TestContent_EmptyTextOmitsTextField(t *testing.T) {
	out, err := json.Marshal(NewTextContent(""))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"type":"text"}` {
		t.Fatalf("empty text = %s, want historical omitempty shape", out)
	}
}

func TestContent_KnownExtraKeyNotDuplicated(t *testing.T) {
	block := Content{
		Type:  "image",
		Data:  "abc",
		Extra: map[string]json.RawMessage{"data": json.RawMessage(`"nope"`), "x": json.RawMessage(`1`)},
	}
	out, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(out), `"data"`) != 1 {
		t.Fatalf("known key duplicated: %s", out)
	}
	assertJSONEqual(t, out, []byte(`{"type":"image","data":"abc","x":1}`))
}

func TestContent_StaleExtraCleared(t *testing.T) {
	block := Content{Extra: map[string]json.RawMessage{"old": json.RawMessage(`1`)}}
	if err := json.Unmarshal([]byte(`{"type":"text","text":"hi"}`), &block); err != nil {
		t.Fatal(err)
	}
	if block.Extra != nil || block.Text != "hi" {
		t.Fatalf("stale extra retained: %+v", block)
	}
}

func TestTextOnlyWireShapeUnchanged(t *testing.T) {
	readOnly := true
	cases := []struct {
		name string
		v    any
		want string
	}{
		{"text content", NewTextContent("hello"), `{"type":"text","text":"hello"}`},
		{"quoted text", NewTextContent(`{"a":1}`), `{"type":"text","text":"{\"a\":1}"}`},
		{"tool", Tool{Name: "echo", Description: "Echoes", InputSchema: json.RawMessage(`{"type":"object"}`)}, `{"name":"echo","description":"Echoes","inputSchema":{"type":"object"}}`},
		{"tool no description", Tool{Name: "t", InputSchema: json.RawMessage(`{}`)}, `{"name":"t","inputSchema":{}}`},
		{"annotations", ToolAnnotations{Title: "Read", ReadOnlyHint: &readOnly}, `{"title":"Read","readOnlyHint":true}`},
		{"result", &ToolCallResult{Content: []Content{NewTextContent("hello")}}, `{"content":[{"type":"text","text":"hello"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestTool_DefinitionRoundTrip(t *testing.T) {
	var tool Tool
	if err := json.Unmarshal([]byte(toolDefinitionWire), &tool); err != nil {
		t.Fatal(err)
	}
	if tool.Title != "Render" || tool.Annotations == nil || tool.Annotations.ReadOnlyHint == nil || !*tool.Annotations.ReadOnlyHint {
		t.Fatalf("typed fields lost: %+v", tool)
	}
	if string(tool.Annotations.Extra["x-custom"]) != `"keep"` {
		t.Fatalf("unknown annotation key lost: %+v", tool.Annotations.Extra)
	}
	if _, known := tool.Extra["icons"]; known {
		t.Fatal("icons captured as an unknown key")
	}
	if string(tool.Extra["futureField"]) != "1" {
		t.Fatalf("unknown tool key lost: %+v", tool.Extra)
	}
	out, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, out, []byte(toolDefinitionWire))
}

func TestGateway_MixedContentSurvivesTruncationAndFormat(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	g.SetMaxToolResultBytes(100)
	g.SetDefaultOutputFormat("toon")

	image := Content{
		Type:        "image",
		Data:        strings.Repeat("A", 200),
		MimeType:    "image/png",
		Annotations: json.RawMessage(`{"audience":["user"]}`),
		Meta:        json.RawMessage(`{"k":"v"}`),
	}
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "shot", Description: "takes a shot"}})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ToolCallResult{
		Content: []Content{
			NewTextContent(`{"name":"Ada"}`),
			image,
			NewTextContent(strings.Repeat("x", 500)),
		},
	}, nil).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "agent1"})

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{
		Name:      "agent1__shot",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 3 {
		t.Fatalf("content blocks changed: %+v", result.Content)
	}
	if result.Content[0].Text == `{"name":"Ada"}` || result.Content[0].Type != "text" {
		t.Fatalf("text block was not converted: %+v", result.Content[0])
	}
	got := result.Content[1]
	if got.Type != "image" || got.Data != image.Data || got.MimeType != image.MimeType {
		t.Fatalf("image block changed: %+v", got)
	}
	if string(got.Annotations) != string(image.Annotations) || string(got.Meta) != string(image.Meta) {
		t.Fatalf("image annotations or _meta changed: %+v", got)
	}
	if !strings.Contains(result.Content[2].Text, "[truncated:") {
		t.Fatalf("text block was not clipped: %s", result.Content[2].Text)
	}
	if !strings.Contains(logs.String(), "non-text content block exceeds result size limit") {
		t.Fatalf("missing oversized non-text debug log: %s", logs.String())
	}
	if strings.Contains(logs.String(), image.Data) {
		t.Fatal("debug log included image payload")
	}
}

func TestGateway_SmallNonTextDoesNotLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	var logs bytes.Buffer
	g.SetLogger(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	g.SetMaxToolResultBytes(100)

	client := setupMockAgentClient(ctrl, "agent1", []Tool{{Name: "shot"}})
	client.EXPECT().CallTool(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ToolCallResult{
		Content: []Content{{Type: "image", Data: "abcd", MimeType: "image/png"}},
	}, nil).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.SetServerMeta(MCPServerConfig{Name: "agent1"})

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "agent1__shot", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content[0].Data != "abcd" {
		t.Fatalf("small image changed: %+v", result.Content[0])
	}
	if strings.Contains(logs.String(), "non-text content block exceeds result size limit") {
		t.Fatalf("small image logged as oversized: %s", logs.String())
	}
}

func TestGateway_ToolsListAndDiscoverPreserveOpaqueFields(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	meta := json.RawMessage(`{"ui":{"resourceUri":"ui://app"}}`)
	icons := json.RawMessage(`[{"src":"icon.png"}]`)
	execution := json.RawMessage(`{"taskSupport":"optional"}`)
	client := setupMockAgentClient(ctrl, "agent1", []Tool{{
		Name:        "render",
		Title:       "Render canvas",
		Description: "draws",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Icons:       icons,
		Execution:   execution,
		Meta:        meta,
		Annotations: &ToolAnnotations{
			ReadOnlyHint: boolPtr(true),
			Extra:        map[string]json.RawMessage{"x-custom": json.RawMessage(`"keep"`)},
		},
	}})
	g.Router().AddClient(client)
	g.Router().RefreshTools()

	list, err := g.HandleToolsList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	discovered, err := g.DiscoverTools(context.Background(), DiscoverOptions{Name: "agent1__render"})
	if err != nil {
		t.Fatal(err)
	}
	for _, site := range []struct {
		name  string
		tools []Tool
	}{
		{"tools/list", list.Tools},
		{"discover", discovered.Tools},
	} {
		if len(site.tools) != 1 {
			t.Fatalf("%s returned %d tools", site.name, len(site.tools))
		}
		tool := site.tools[0]
		if tool.Title != "Render canvas" {
			t.Fatalf("%s title = %q", site.name, tool.Title)
		}
		if string(tool.Meta) != string(meta) || string(tool.Icons) != string(icons) || string(tool.Execution) != string(execution) {
			t.Fatalf("%s dropped opaque fields: %+v", site.name, tool)
		}
		if tool.Annotations == nil || string(tool.Annotations.Extra["x-custom"]) != `"keep"` {
			t.Fatalf("%s dropped unknown annotation key", site.name)
		}
	}
}

func assertJSONEqual(t *testing.T, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got: %v (%s)", err, got)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatalf("want: %v (%s)", err, want)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("json mismatch\ngot:  %s\nwant: %s", got, want)
	}
}
