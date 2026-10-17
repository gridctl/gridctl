package config

import (
	"strings"
	"testing"
)

func TestDecodeStack_UnknownKeys(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		want    string
		line    int
		wantKey string
	}{
		{
			name: "top level",
			yaml: "version: \"1\"\nname: demo\nheaderz: true\nmcp-servers: []\n",
			want: `unknown field "headerz" (line 3)`,
		},
		{
			name: "server key",
			yaml: "version: \"1\"\nname: demo\nmcp-servers:\n  - name: api\n    url: https://example.test/mcp\n    headerz: true\n",
			want: "mcp-servers[0].headerz",
		},
		{
			name: "gateway",
			yaml: "version: \"1\"\nname: demo\ngateway:\n  insecure_allow_authenticated: true\nmcp-servers: []\n",
			want: "gateway.insecure_allow_authenticated",
		},
		{
			name: "network",
			yaml: "version: \"1\"\nname: demo\nnetwork:\n  name: demo\n  type: bridge\nmcp-servers: []\n",
			want: "network.type",
		},
		{
			name: "group override",
			yaml: "version: \"1\"\nname: demo\nmcp-servers:\n  - name: github\n    url: https://example.test/mcp\ngroups:\n  ops:\n    tools: [github__list]\n    overrides:\n      github__x:\n        foo: bar\n",
			want: "groups[ops].overrides[github__x].foo",
		},
		{
			name: "link mapping",
			yaml: "version: \"1\"\nname: demo\nmcp-servers: []\nlink:\n  - client: cursor\n    nope: true\n",
			want: "link[0].nope",
		},
		{
			name: "a2a auth",
			yaml: "version: \"1\"\nname: demo\nexperimental:\n  a2a: true\nmcp-servers:\n  - name: agent\n    a2a:\n      card: https://example.test/card\n      auth:\n        type: bearer\n        token: ${var:TOKEN}\n        extra: 1\n",
			want: "mcp-servers[0].a2a.auth.extra",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DecodeStack([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected unknown field error")
			}
			var ve ValidationErrors
			if !errorsAsValidation(err, &ve) {
				t.Fatalf("error = %v, want ValidationErrors", err)
			}
			found := false
			for _, item := range ve {
				if item.Field == tt.want || strings.Contains(item.Message, tt.want) || item.Field+" "+item.Message == tt.want {
					found = true
				}
				if strings.Contains(item.Field, tt.want) || item.Message == tt.want || item.Field == tt.want {
					found = true
					issue := item.ToIssue(SeverityError)
					if issue.Line == 0 {
						t.Errorf("issue line = 0, message %q", item.Message)
					}
				}
			}
			if !found {
				t.Fatalf("errors = %v, want %q", ve, tt.want)
			}
		})
	}
}

func TestDecodeStack_Empty(t *testing.T) {
	stack, err := DecodeStack(nil)
	if err != nil {
		t.Fatal(err)
	}
	if stack == nil || stack.Name != "" {
		t.Fatalf("stack = %+v", stack)
	}
	stack, err = DecodeStack([]byte("\n"))
	if err != nil {
		t.Fatal(err)
	}
	if stack == nil {
		t.Fatal("nil stack")
	}
	stack, err = DecodeStack([]byte("# only a comment\n"))
	if err != nil {
		t.Fatalf("comment-only input: %v", err)
	}
	if stack == nil || stack.Name != "" {
		t.Fatalf("comment-only stack = %+v", stack)
	}
}

func TestDecodeStack_AcceptsDescriptionAndHeaders(t *testing.T) {
	raw := []byte("version: \"1\"\nname: demo\nmcp-servers:\n  - name: api\n    url: https://example.test/mcp\n    description: notes\n    headers:\n      X-Tenant: ${var:TENANT}\n")
	stack, err := DecodeStack(raw)
	if err != nil {
		t.Fatal(err)
	}
	if stack.MCPServers[0].Description != "notes" {
		t.Fatalf("description = %q", stack.MCPServers[0].Description)
	}
	if stack.MCPServers[0].Headers["X-Tenant"] != "${var:TENANT}" {
		t.Fatalf("headers = %#v", stack.MCPServers[0].Headers)
	}
}

func errorsAsValidation(err error, ve *ValidationErrors) bool {
	got, ok := err.(ValidationErrors)
	if !ok {
		return false
	}
	*ve = got
	return true
}
