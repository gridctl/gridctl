package servercard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestBuild_ValidatesAgainstVendoredSchema(t *testing.T) {
	schema := compileServerCard(t)
	for _, id := range []Identity{
		{GatewayName: "lab", StackName: "lab", Version: "1.2.3", ProtocolVersions: mcp.SupportedProtocolVersions, Origin: "http://127.0.0.1:8180"},
		{GatewayName: "lab", StackName: "lab", Version: "1.2.3", Group: "release", ProtocolVersions: mcp.SupportedProtocolVersions, Origin: "http://127.0.0.1:8180", AuthType: "bearer"},
	} {
		card, err := Build(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(mustInstance(t, card)); err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
}

func TestValidate_RejectsBounds(t *testing.T) {
	card, err := Build(Identity{GatewayName: "lab", StackName: "lab", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	card.Name = "not a name"
	if err := Validate(card); err == nil {
		t.Fatal("bad name accepted")
	}
	card.Name = "io.gridctl/lab"
	card.Schema = ""
	if err := Validate(card); err == nil {
		t.Fatal("missing schema accepted")
	}
	card.Schema = SchemaURL
	card.Description = strings.Repeat("a", 101)
	if err := Validate(card); err == nil {
		t.Fatal("long description accepted")
	}
}

func TestBuild_SanitizesGatewayNames(t *testing.T) {
	long := strings.Repeat("n", 100)
	for _, name := range []string{"a/b", "my gateway", long} {
		card, err := Build(Identity{GatewayName: name, StackName: "lab", Version: "1", Group: "ops"})
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if err := Validate(card); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
	}
}

func TestBuild_UntrustedHostShape(t *testing.T) {
	card, err := Build(Identity{GatewayName: "lab", StackName: "lab", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(card.Remotes) != 0 {
		t.Fatalf("remotes = %#v", card.Remotes)
	}
	cat := BuildCatalog("", "", "lab", nil, nil)
	if len(cat.Entries) != 0 {
		t.Fatalf("entries = %#v", cat.Entries)
	}
	pinned, err := Build(Identity{GatewayName: "lab", StackName: "lab", Version: "1", Origin: "https://gw.example", PathPrefix: "/prefix"})
	if err != nil {
		t.Fatal(err)
	}
	if pinned.Remotes[0].URL != "https://gw.example/prefix/mcp" {
		t.Fatalf("url = %s", pinned.Remotes[0].URL)
	}
}

func TestBuild_AuthHeaders(t *testing.T) {
	bearer, err := Build(Identity{GatewayName: "lab", StackName: "lab", Version: "1", Origin: "http://127.0.0.1:1", AuthType: "bearer"})
	if err != nil {
		t.Fatal(err)
	}
	header := bearer.Remotes[0].Headers[0]
	if header.Value != "Bearer {token}" || !header.Variables["token"].IsRequired || !header.Variables["token"].IsSecret {
		t.Fatalf("bearer header = %#v", header)
	}
	draft, err := DraftFromCard(bearer, "http://127.0.0.1:1/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if draft.Entry.Install.AuthType != "bearer" || len(draft.Entry.Inputs) != 1 || !draft.Entry.Inputs[0].Required || !draft.Entry.Inputs[0].Secret {
		t.Fatalf("draft = %#v", draft)
	}
	server, _, err := draft.ServerDraft("lab")
	if err != nil {
		t.Fatal(err)
	}
	if server.Auth == nil || server.Auth.Type != "bearer" || !strings.HasPrefix(server.Auth.Token, "${var:") {
		t.Fatalf("auth = %#v", server.Auth)
	}

	apiKey, err := Build(Identity{GatewayName: "lab", StackName: "lab", Version: "1", Origin: "http://127.0.0.1:1", AuthType: "api_key"})
	if err != nil {
		t.Fatal(err)
	}
	got := apiKey.Remotes[0].Headers[0]
	if got.Name != "Authorization" || got.Value != "{key}" || strings.Contains(got.Value, "Bearer") {
		t.Fatalf("api_key header = %#v", got)
	}
}

func TestProtocolGeneration_MixedVersions(t *testing.T) {
	card, err := Build(Identity{
		GatewayName: "lab", StackName: "lab", Version: "1", Origin: "http://127.0.0.1:1",
		ProtocolVersions: []string{"2025-11-25", "9999-01-01"},
	})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := DraftFromCard(card, "http://127.0.0.1:1/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if draft.ProtocolGeneration != "" {
		t.Fatalf("generation = %q", draft.ProtocolGeneration)
	}
}

func TestFetch_Bounds(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/go" {
			http.Redirect(w, r, "/stop", http.StatusFound)
			return
		}
		_, _ = w.Write(make([]byte, 80))
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.URL+"/go", FetchOptions{}); err == nil {
		t.Fatal("redirect was followed")
	}
	if _, err := Fetch(context.Background(), srv.URL+"/big", FetchOptions{MaxBytes: 16}); err == nil {
		t.Fatal("oversized body accepted")
	}
	if sawAuth != "" {
		t.Fatalf("sent Authorization %q", sawAuth)
	}
}

func compileServerCard(t *testing.T) interface{ Validate(any) error } {
	t.Helper()
	sch, err := CompileVendoredSchema("testdata/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func mustInstance(t *testing.T, card *ServerCard) any {
	t.Helper()
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
