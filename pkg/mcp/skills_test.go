package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"go.uber.org/mock/gomock"
)

type testSkillProvider struct {
	reads     []string
	listAllow func(string) bool
}

func (p *testSkillProvider) ListSkillEntries(_ context.Context, allow func(string) bool) ([]SkillEntry, error) {
	if allow != nil {
		p.listAllow = allow
	}
	return []SkillEntry{
		{Name: "denied", URI: SkillURI("denied", "SKILL.md"), Frontmatter: json.RawMessage(`{"name":"denied","description":"Hidden"}`)},
		{Name: "alpha", URI: SkillURI("alpha", "SKILL.md"), Frontmatter: json.RawMessage(`{"name":"alpha","description":"Alpha"}`)},
	}, nil
}
func (p *testSkillProvider) GetSkillEntry(ctx context.Context, name string) (*SkillEntry, error) {
	p.reads = append(p.reads, name)
	entries, _ := p.ListSkillEntries(ctx, nil)
	for _, entry := range entries {
		if entry.Name == name {
			return &entry, nil
		}
	}
	return nil, ErrSkillNotFound
}
func (p *testSkillProvider) ReadSkillFile(ctx context.Context, name, path string) (*SkillFileContent, error) {
	if _, err := p.GetSkillEntry(ctx, name); err != nil {
		return nil, err
	}
	switch path {
	case "SKILL.md":
		return &SkillFileContent{MimeType: "text/markdown", Text: "alpha"}, nil
	case "empty.txt":
		return &SkillFileContent{MimeType: "text/plain"}, nil
	case "logo.png":
		return &SkillFileContent{MimeType: "image/png", Blob: []byte{0, 255}}, nil
	default:
		return nil, ErrSkillNotFound
	}
}
func (p *testSkillProvider) ReadSkillDirectory(ctx context.Context, name, dir string) ([]SkillDirectoryEntry, error) {
	if _, err := p.GetSkillEntry(ctx, name); err != nil {
		return nil, err
	}
	if dir != "" {
		return nil, ErrSkillNotFound
	}
	return []SkillDirectoryEntry{{URI: SkillURI(name, "SKILL.md"), Name: name, Description: "Alpha", MimeType: "text/markdown"}}, nil
}

func skillsGateway(t *testing.T) (*Gateway, *testSkillProvider) {
	t.Helper()
	g := NewGateway()
	client := &promptProviderClient{AgentClient: setupMockAgentClient(gomock.NewController(t), "registry", nil),
		prompts: []PromptData{{Name: "alpha", Description: "Alpha"}, {Name: "denied", Description: "Hidden"}}}
	g.Router().AddClient(client)
	p := &testSkillProvider{}
	g.SetSkillProvider(p)
	g.SetSkillPolicy(NewSkillPolicy(&SkillPolicySpec{Deny: []string{"denied"}}))
	return g, p
}

func TestSkills_DeclarationAndNegotiation(t *testing.T) {
	g, _ := skillsGateway(t)
	ctx := context.Background()
	caps := g.HandleServerDiscover(ctx).Capabilities
	if string(caps.Extensions[SkillsExtensionID]) != `{"directoryRead":true}` || caps.Resources == nil {
		t.Fatalf("capabilities: %+v", caps)
	}
	g.Router().AddClient(taskCapableFake("tasks"))
	if len(g.HandleServerDiscover(ctx).Capabilities.Extensions) != 2 {
		t.Fatal("tasks replaced skills declaration")
	}
	params := InitializeParams{ProtocolVersion: "2025-11-25", ClientInfo: ClientInfo{Name: "host"}}
	with, session, err := g.HandleInitialize(params, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if session.SkillsExtension {
		t.Fatal("undeclared session opted in")
	}
	g.SetSkillProvider(nil)
	without, _, err := g.HandleInitialize(params, "", "")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(with)
	b, _ := json.Marshal(without)
	if string(a) != string(b) {
		t.Fatalf("legacy initialize changed:\n%s\n%s", a, b)
	}
	g.SetSkillProvider(&testSkillProvider{})
	params.Capabilities.Extensions = map[string]json.RawMessage{SkillsExtensionID: json.RawMessage(`{}`)}
	for _, principal := range []bool{false, true} {
		var result *InitializeResult
		if principal {
			result, session, err = g.HandleInitializePrincipal(params, "host", "subject", "")
		} else {
			result, session, err = g.HandleInitialize(params, "", "")
		}
		if err != nil || !session.SkillsExtension || result.Capabilities.Extensions[SkillsExtensionID] == nil {
			t.Fatalf("negotiated: %+v, %v", result, err)
		}
	}
	g.Router().RemoveClient("registry")
	if g.skillProvider() != nil || g.HandleServerDiscover(ctx).Capabilities.Extensions[SkillsExtensionID] != nil {
		t.Fatal("registry removal retained extension")
	}
	if _, err := g.HandleSkillsList(ctx, ""); !errors.Is(err, errSkillsUnavailable) {
		t.Fatal(err)
	}
}

func TestSkills_PolicyAndResources(t *testing.T) {
	g, p := skillsGateway(t)
	ctx := context.Background()
	list, err := g.HandleSkillsList(ctx, "")
	if err != nil || len(list.Skills) != 1 || list.Skills[0].Name != "alpha" {
		t.Fatalf("list: %+v %v", list, err)
	}
	if p.listAllow == nil || p.listAllow("denied") || !p.listAllow("alpha") {
		t.Fatal("skills/list did not apply policy before the provider")
	}
	a, _ := json.Marshal(list)
	list, _ = g.HandleSkillsList(ctx, "")
	b, _ := json.Marshal(list)
	if string(a) != string(b) || !strings.Contains(string(a), `"ttlMs":60000`) {
		t.Fatalf("unstable metadata: %s / %s", a, b)
	}
	for _, uri := range []string{SkillURI("denied", "SKILL.md"), SkillURI("missing", "SKILL.md"), "skill://alpha/SKILL.md", "skill://registry/alpha/"} {
		_, err := g.HandleSkillsGet(ctx, uri)
		var nf *ResourceNotFoundError
		if !errors.As(err, &nf) || nf.URI != uri || nf.Message != "No skill is served at "+uri {
			t.Fatalf("get %s: %v", uri, err)
		}
	}
	if !reflect.DeepEqual(p.reads, []string{"missing"}) {
		t.Fatalf("policy touched provider: %v", p.reads)
	}
	for _, tc := range []struct{ path, want string }{{"SKILL.md", `"text":"alpha"`}, {"empty.txt", `"text":""`}, {"logo.png", `"blob":"AP8="`}} {
		raw, err := g.HandleResourcesRead(ctx, ResourcesReadParams{URI: SkillURI("alpha", tc.path)})
		if err != nil || !strings.Contains(string(raw), tc.want) {
			t.Fatalf("read: %s %v", raw, err)
		}
	}
	resources, err := g.HandleResourcesList(ctx)
	if err != nil || len(resources.Resources) != 2 {
		t.Fatalf("resources: %+v %v", resources, err)
	}
	if !strings.Contains(string(resources.Resources[0]), "skills://registry/alpha") || !strings.Contains(string(resources.Resources[1]), "skill://registry/alpha/SKILL.md") {
		t.Fatalf("resource order: %s", resources.Resources)
	}
	g.SetSkillPolicy(nil)
	list, _ = g.HandleSkillsList(ctx, "")
	if len(list.Skills) != 2 || list.Skills[0].Name != "alpha" {
		t.Fatalf("reload: %+v", list)
	}
	g.SetSkillPolicy(NewSkillPolicy(&SkillPolicySpec{Deny: []string{"*"}}))
	list, _ = g.HandleSkillsList(ctx, "")
	b, _ = json.Marshal(list)
	if !strings.Contains(string(b), `"skills":[]`) {
		t.Fatalf("empty: %s", b)
	}
}

func TestSkills_EdgeGatingAndErrors(t *testing.T) {
	g, _ := skillsGateway(t)
	s := NewStreamableHTTPServer(g, nil)
	ctx := context.Background()
	params := InitializeParams{ProtocolVersion: "2025-11-25"}
	_, legacy, _ := g.HandleInitialize(params, "", "")
	params.Capabilities.Extensions = map[string]json.RawMessage{SkillsExtensionID: json.RawMessage(`{}`)}
	_, opted, _ := g.HandleInitialize(params, "", "")
	for _, tc := range []struct {
		method, params string
		code           int
	}{
		{"skills/list", `{}`, 0},
		{"skills/get", `{"uri":"skill://registry/alpha/SKILL.md"}`, 0},
		{"resources/directory/read", `{"uri":"skill://registry/alpha"}`, 0},
		{"skills/list", `{"cursor":"bad"}`, -32602},
		{"resources/directory/read", `{"uri":"skill://registry/alpha","cursor":"bad"}`, -32602},
		{"skills/get", `{"uri":"skill://registry/denied/SKILL.md"}`, -32602},
		{"resources/directory/read", `{"uri":"skill://registry/alpha/SKILL.md"}`, -32602},
		{"resources/directory/read", `{"uri":"skill://registry/alpha/"}`, -32602},
	} {
		req := &jsonrpc.Request{Method: tc.method, Params: json.RawMessage(tc.params)}
		resp := s.handleRequest(ctx, &StreamableSession{ID: legacy.ID}, req)
		if resp.Error == nil || resp.Error.Code != -32601 {
			t.Fatalf("legacy %s: %+v", tc.method, resp)
		}
		for _, handshake := range []bool{false, true} {
			if handshake {
				resp = s.handleRequest(ctx, &StreamableSession{ID: opted.ID}, req)
			} else {
				resp = s.handleSkillsRequest(ctx, req, false)
			}
			code := 0
			if resp.Error != nil {
				code = resp.Error.Code
			}
			if code != tc.code {
				t.Fatalf("%s %s handshake=%v: %+v", tc.method, tc.params, handshake, resp)
			}
			if code == 0 {
				raw, _ := json.Marshal(resp.Result)
				if strings.Contains(string(raw), "ttlMs") == handshake {
					t.Fatalf("cache fields: %s", raw)
				}
			}
		}
	}
	for _, uri := range []string{"skill://registry/alpha/.hidden", "skill://registry/alpha/scripts/link", "skill://registry/alpha/../denied/SKILL.md", "skill://registry/denied/SKILL.md"} {
		params, _ := json.Marshal(ResourcesReadParams{URI: uri})
		resp := s.handleRequest(ctx, &StreamableSession{ID: legacy.ID}, &jsonrpc.Request{Method: "resources/read", Params: params})
		if resp.Error == nil || resp.Error.Code != -32002 {
			t.Fatalf("read miss: %+v", resp)
		}
	}
	g.Router().RemoveClient("registry")
	for _, method := range []string{"skills/list", "skills/get", "resources/directory/read"} {
		resp := s.handleSkillsRequest(ctx, &jsonrpc.Request{Method: method}, false)
		if resp.Error == nil || resp.Error.Code != -32601 {
			t.Fatalf("unavailable: %+v", resp)
		}
	}
}
