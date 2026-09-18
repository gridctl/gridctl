package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/a2aclient"
)

func TestA2AEnvelope_PreservesApplicationBoundaries(t *testing.T) {
	part := a2aclient.Part{Type: "data", Data: map[string]any{"number": json.Number("9007199254740993"), "empty": map[string]any{}}}
	wire := a2aclient.Result{
		Kind: "task", TaskID: "remote-task", ContextID: "remote-context", State: "input-required",
		StatusMessages: []a2aclient.Message{{Role: "agent", TaskID: "remote-task", ContextID: "remote-context", Parts: []a2aclient.Part{part}}},
		Artifacts: []a2aclient.Artifact{
			{Name: "first", Description: "description", Parts: []a2aclient.Part{{Type: "text", Text: ""}, part}},
			{Name: "second", Parts: []a2aclient.Part{{Type: "text", Text: "hello"}}},
		},
		History: []a2aclient.Message{{Role: "user", Parts: []a2aclient.Part{{Type: "text", Text: "input"}}}},
	}
	envelope, err := prepareA2AEnvelope(wire, 0)
	if err != nil {
		t.Fatal(err)
	}
	delivery := capabilityDelivery{taskHandle: "caller-task", contextHandle: "caller-context", taskExpires: time.Now(), contextExpires: time.Now()}
	result, err := envelope.deliver(delivery)
	if err != nil {
		t.Fatal(err)
	}
	body := result.Content[0].Text
	if strings.Contains(body, "remote-") || !strings.Contains(body, "9007199254740993") || !strings.Contains(body, `"text":""`) || !result.atomicResult {
		t.Fatal("lost precision, leaked routing, or lost delivery marker")
	}
	var decoded a2aEnvelope
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.State != "input-required" || len(decoded.Parts) != 0 || len(decoded.Artifacts) != 2 || len(decoded.StatusMessages) != 1 || len(decoded.History) != 1 || decoded.Artifacts[1].Name != "second" {
		t.Fatalf("lost boundaries: %+v", decoded)
	}
	if result.ResultType != "" || result.RequestState != "" || len(result.InputRequests) != 0 {
		t.Fatal("A2A state became MCP continuation")
	}
	refs := a2aAdmissionResponse(wire)
	if refs.taskID != wire.TaskID || refs.contextID != wire.ContextID || len(refs.references) != 2 || refs.references[0].taskID != wire.TaskID {
		t.Fatal("admission lost protocol references")
	}
}

func TestA2AEnvelope_BoundedAtomicDelivery(t *testing.T) {
	wire := a2aclient.Result{Kind: "message", Parts: []a2aclient.Part{
		{Type: "text", Text: "first"},
		{Type: "data", Data: map[string]any{"large": strings.Repeat("x", 1<<20)}},
		{Type: "text", Text: "last"},
	}}
	for _, limit := range []int{512, 1024, 65536} {
		envelope, err := prepareA2AEnvelope(wire, limit)
		if err != nil {
			t.Fatal(err)
		}
		if !envelope.ContentTruncated || len(envelope.Parts) != 1 || envelope.Parts[0].Text != "first" {
			t.Fatal("truncation did not preserve semantic prefix")
		}
		delivery := capabilityDelivery{taskHandle: strings.Repeat("t", 52), contextHandle: strings.Repeat("c", 52), taskExpires: time.Now(), contextExpires: time.Now()}
		result, err := envelope.deliver(delivery)
		if err != nil {
			t.Fatal(err)
		}
		body := result.Content[0].Text
		if len(body) > limit || !json.Valid([]byte(body)) || !strings.Contains(body, delivery.taskHandle) || !strings.Contains(body, delivery.contextHandle) {
			t.Fatal("result budget lost handles or valid JSON")
		}
	}
	if _, err := prepareA2AEnvelope(wire, 10); err == nil {
		t.Fatal("small budget accepted")
	}
	if err := checkA2AResultBudget(10); err == nil {
		t.Fatal("small preflight budget accepted")
	}
	if err := checkA2AResultBudget(-1); err != nil {
		t.Fatal(err)
	}
	full, err := prepareA2AEnvelope(wire, -1)
	if err != nil || full.ContentTruncated || len(full.Parts) != 3 {
		t.Fatal("unlimited budget changed application content", err)
	}
}

func TestA2AEnvelope_TaskTruncationAndTaskOnlyDelivery(t *testing.T) {
	wire := a2aclient.Result{Kind: "task", State: "working",
		StatusMessages: []a2aclient.Message{{Role: "agent", Parts: []a2aclient.Part{{Type: "text", Text: "working"}}}},
		Artifacts:      []a2aclient.Artifact{{Name: "large", Parts: []a2aclient.Part{{Type: "text", Text: strings.Repeat("x", 10000)}}}},
		History:        []a2aclient.Message{{Role: "user", Parts: []a2aclient.Part{{Type: "text", Text: "prior"}}}},
	}
	envelope, err := prepareA2AEnvelope(wire, 512)
	if err != nil {
		t.Fatal(err)
	}
	if len(envelope.StatusMessages) != 1 || len(envelope.Artifacts) != 0 || len(envelope.History) != 0 || !envelope.ContentTruncated {
		t.Fatal("task boundaries were clipped")
	}
	result, err := envelope.deliver(capabilityDelivery{taskHandle: "supplied-task", taskExpires: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Content[0].Text, "context_") || !strings.Contains(result.Content[0].Text, "supplied-task") {
		t.Fatal("task-only delivery revealed context")
	}
	message, err := prepareA2AEnvelope(a2aclient.Result{Kind: "message", Parts: []a2aclient.Part{{Type: "data", Data: map[string]any{}}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	result, err = message.deliver(capabilityDelivery{})
	if err != nil || strings.Contains(result.Content[0].Text, "handle") || !strings.Contains(result.Content[0].Text, `"data":{}`) {
		t.Fatal("empty data or absent authority changed", err)
	}
}

func TestA2AEnvelope_InvalidPartsFailBeforeCommit(t *testing.T) {
	for _, wire := range []a2aclient.Result{
		{Kind: "other"},
		{Kind: "message", Parts: []a2aclient.Part{{Type: "file"}}},
		{Kind: "message", Parts: []a2aclient.Part{{Type: "data"}}},
	} {
		if _, err := prepareA2AEnvelope(wire, 512); err == nil {
			t.Fatal("invalid result accepted")
		}
	}
}

func TestA2AEnvelope_GatewayPreservesAtomicResult(t *testing.T) {
	g := NewGateway()
	g.SetMaxToolResultBytes(1)
	g.SetDefaultOutputFormat("toon")
	result := &ToolCallResult{Content: []Content{NewTextContent(`{"kind":"message","state":""}`)}, atomicResult: true}
	original := result.Content[0].Text
	g.applyTruncation("agent", "send", result)
	g.applyFormatConversion(context.Background(), "agent", result)
	if result.Content[0].Text != original {
		t.Fatal("gateway rewrote atomic envelope")
	}
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), "atomic") {
		t.Fatal("internal trust marker was serialized", err)
	}
	var remote ToolCallResult
	if err := json.Unmarshal([]byte(`{"content":[],"atomicResult":true}`), &remote); err != nil {
		t.Fatal(err)
	}
	if remote.atomicResult {
		t.Fatal("untrusted wire result set internal trust marker")
	}
	ordinary := &ToolCallResult{Content: []Content{NewTextContent(original)}}
	g.applyFormatConversion(context.Background(), "agent", ordinary)
	if ordinary.Content[0].Text == original {
		t.Fatal("control did not exercise configured format conversion")
	}
}
