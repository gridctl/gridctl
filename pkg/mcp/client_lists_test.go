package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestDrainListPageBound(t *testing.T) {
	var calls int
	var logs bytes.Buffer
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			if method != "resources/list" {
				t.Fatalf("method = %s", method)
			}
			calls++
			raw, err := json.Marshal(listEnvelope{
				Resources:  []json.RawMessage{json.RawMessage(`{"uri":"file:///p"}`)},
				NextCursor: "again",
			})
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, result)
		},
	}
	r := newFakeRPCClient("s", ft)
	r.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	page, err := r.ListResources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls != maxListPages {
		t.Fatalf("calls = %d, want %d", calls, maxListPages)
	}
	if len(page.Entries) != maxListPages {
		t.Fatalf("entries = %d, want %d", len(page.Entries), maxListPages)
	}
	if !strings.Contains(logs.String(), "downstream list page limit reached") {
		t.Fatalf("log = %s", logs.String())
	}
}

func TestListMethodsDrainOnePage(t *testing.T) {
	ft := &fakeTransport{
		callFn: func(_ context.Context, method string, _ any, result any) error {
			env := listEnvelope{CacheScope: CacheScopePublic, TTLMs: func() *int64 { n := int64(5); return &n }()}
			switch method {
			case "resources/list":
				env.Resources = []json.RawMessage{json.RawMessage(`{"uri":"file:///a"}`)}
			case "resources/templates/list":
				env.ResourceTemplates = []json.RawMessage{json.RawMessage(`{"uriTemplate":"file:///{name}"}`)}
			case "prompts/list":
				env.Prompts = []json.RawMessage{json.RawMessage(`{"name":"review"}`)}
			default:
				t.Fatalf("method = %s", method)
			}
			raw, err := json.Marshal(env)
			if err != nil {
				return err
			}
			return json.Unmarshal(raw, result)
		},
	}
	r := newFakeRPCClient("s", ft)
	r.SetEra(EraStateless)
	resources, err := r.ListResources(context.Background())
	if err != nil || len(resources.Entries) != 1 {
		t.Fatalf("resources = %+v %v", resources, err)
	}
	templates, err := r.ListResourceTemplates(context.Background())
	if err != nil || len(templates.Entries) != 1 {
		t.Fatalf("templates = %+v %v", templates, err)
	}
	prompts, err := r.ListPrompts(context.Background())
	if err != nil || len(prompts.Entries) != 1 {
		t.Fatalf("prompts = %+v %v", prompts, err)
	}
}
