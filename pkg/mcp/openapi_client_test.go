package mcp

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestLoadSpec_HTMLContentType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Not a spec</body></html>"))
	}))
	defer srv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/openapi.json",
		BaseURL: srv.URL,
	})

	err := client.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error for HTML response")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "text/html") {
		t.Errorf("error should mention content type, got: %s", errMsg)
	}
	if strings.Contains(errMsg, "invalid character") {
		t.Errorf("error should not show parser error, got: %s", errMsg)
	}
}

func TestLoadSpec_JSONContentType(t *testing.T) {
	spec := `{
		"openapi": "3.0.3",
		"info": {"title": "Test", "version": "1.0.0"},
		"servers": [{"url": "http://localhost"}],
		"paths": {}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	defer srv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/openapi.json",
		BaseURL: srv.URL,
	})

	err := client.Initialize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitialize_ValidationWarningNonFatal(t *testing.T) {
	// OpenAPI 3.1 uses type: "null" in anyOf which kin-openapi doesn't fully support.
	// Validation should warn but not block initialization.
	spec := `{
		"openapi": "3.1.0",
		"info": {"title": "Test", "version": "1.0.0"},
		"servers": [{"url": "http://localhost"}],
		"paths": {},
		"components": {
			"schemas": {
				"NullableField": {
					"anyOf": [
						{"type": "string"},
						{"type": "null"}
					]
				}
			}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	defer srv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/openapi.json",
		BaseURL: srv.URL,
	})

	err := client.Initialize(context.Background())
	if err != nil {
		t.Fatalf("validation warning should not block initialization: %v", err)
	}
}

func TestLoadSpec_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/openapi.json",
		BaseURL: srv.URL,
	})

	err := client.Initialize(context.Background())
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("error should mention status code, got: %s", err.Error())
	}
}

func TestNewOpenAPIClient_Name(t *testing.T) {
	c, err := NewOpenAPIClient("my-api", &OpenAPIClientConfig{
		Spec:    "http://example.com/spec.json",
		BaseURL: "http://example.com",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Name() != "my-api" {
		t.Errorf("expected name 'my-api', got %q", c.Name())
	}
}

func TestNewOpenAPIClient_SetLogger(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    "http://example.com/spec.json",
		BaseURL: "http://example.com",
	})
	// SetLogger with nil should not panic
	c.SetLogger(nil)
	// SetLogger with a real logger
	c.SetLogger(slog.Default())
}

func TestNewOpenAPIClient_IncludeExclude(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    "http://example.com/spec.json",
		BaseURL: "http://example.com",
		Include: []string{"getUser", "createUser"},
		Exclude: []string{"deleteUser"},
	})
	// Include list takes precedence
	if !c.shouldInclude("getUser") {
		t.Error("expected getUser to be included")
	}
	if c.shouldInclude("deleteUser") {
		t.Error("expected deleteUser to NOT be included when include list is set")
	}
	if c.shouldInclude("listUsers") {
		t.Error("expected listUsers to NOT be included when include list is set")
	}
}

func TestShouldInclude_NoFilters(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    "http://example.com/spec.json",
		BaseURL: "http://example.com",
	})
	if !c.shouldInclude("anything") {
		t.Error("expected all operations included when no filters")
	}
}

func TestShouldInclude_ExcludeOnly(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    "http://example.com/spec.json",
		BaseURL: "http://example.com",
		Exclude: []string{"deleteUser"},
	})
	if !c.shouldInclude("getUser") {
		t.Error("expected getUser to be included")
	}
	if c.shouldInclude("deleteUser") {
		t.Error("expected deleteUser to be excluded")
	}
}

func TestSanitizeOpenAPIToolName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "getUser", "getUser"},
		{"with spaces", "get user", "get_user"},
		{"with dots", "api.getUser", "api_getUser"},
		{"with hyphens", "get-user", "get-user"},
		{"with underscores", "get_user", "get_user"},
		{"long name", strings.Repeat("a", 100), strings.Repeat("a", 64)},
		{"all invalid", "...", ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeOpenAPIToolName(tc.input)
			if got != tc.expected {
				t.Errorf("sanitizeOpenAPIToolName(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestContains(t *testing.T) {
	if !contains([]string{"a", "b", "c"}, "b") {
		t.Error("expected contains to find 'b'")
	}
	if contains([]string{"a", "b", "c"}, "d") {
		t.Error("expected contains to not find 'd'")
	}
	if contains(nil, "a") {
		t.Error("expected contains on nil slice to return false")
	}
}

func TestExtractPathParams(t *testing.T) {
	tests := []struct {
		path     string
		expected []string
	}{
		{"/users/{userId}", []string{"userId"}},
		{"/users/{userId}/posts/{postId}", []string{"userId", "postId"}},
		{"/users", nil},
		{"/{org}/{repo}/issues/{id}", []string{"org", "repo", "id"}},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := extractPathParams(tc.path)
			if len(got) == 0 && len(tc.expected) == 0 {
				return
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("expected %d params, got %d", len(tc.expected), len(got))
			}
			for i, v := range got {
				if v != tc.expected[i] {
					t.Errorf("param[%d] = %q, want %q", i, v, tc.expected[i])
				}
			}
		})
	}
}

func TestBuildDescription(t *testing.T) {
	tests := []struct {
		name        string
		summary     string
		description string
		expected    string
	}{
		{"summary only", "Get user", "", "Get user"},
		{"description only", "", "Retrieves a user by ID", "Retrieves a user by ID"},
		{"both", "Get user", "Retrieves a user by ID", "Get user: Retrieves a user by ID"},
		{"neither", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			op := &openapi3.Operation{
				Summary:     tc.summary,
				Description: tc.description,
			}
			got := buildDescription(op)
			if got != tc.expected {
				t.Errorf("buildDescription() = %q, want %q", got, tc.expected)
			}
		})
	}
}

func TestInitialize_WithOperations(t *testing.T) {
	spec := `{
		"openapi": "3.0.3",
		"info": {"title": "Test", "version": "1.0.0"},
		"servers": [{"url": "http://localhost"}],
		"paths": {
			"/users": {
				"get": {
					"operationId": "listUsers",
					"summary": "List all users",
					"parameters": [
						{"name": "limit", "in": "query", "schema": {"type": "integer"}}
					],
					"responses": {"200": {"description": "OK"}}
				}
			},
			"/users/{userId}": {
				"get": {
					"operationId": "getUser",
					"summary": "Get a user",
					"parameters": [
						{"name": "userId", "in": "path", "required": true, "schema": {"type": "string"}}
					],
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	defer srv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/openapi.json",
		BaseURL: srv.URL,
	})

	err := client.Initialize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.RefreshTools(context.Background()); err != nil {
		t.Fatalf("unexpected error on RefreshTools: %v", err)
	}

	tools := client.Tools()
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestInitialize_WithToolWhitelist(t *testing.T) {
	spec := `{
		"openapi": "3.0.3",
		"info": {"title": "Test", "version": "1.0.0"},
		"servers": [{"url": "http://localhost"}],
		"paths": {
			"/users": {
				"get": {
					"operationId": "listUsers",
					"summary": "List users",
					"responses": {"200": {"description": "OK"}}
				}
			},
			"/users/{id}": {
				"get": {
					"operationId": "getUser",
					"summary": "Get user",
					"parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	defer srv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/openapi.json",
		BaseURL: srv.URL,
	})
	client.SetToolWhitelist([]string{"listUsers"})

	err := client.Initialize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.RefreshTools(context.Background()); err != nil {
		t.Fatalf("unexpected error on RefreshTools: %v", err)
	}

	tools := client.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (whitelist filter), got %d", len(tools))
	}
	if tools[0].Name != "listUsers" {
		t.Errorf("expected tool name 'listUsers', got %q", tools[0].Name)
	}
}

func TestCallTool_WithHTTPServer(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"openapi": "3.0.3",
				"info": {"title": "Test", "version": "1.0.0"},
				"servers": [{"url": "` + "REPLACE_URL" + `"}],
				"paths": {
					"/echo": {
						"post": {
							"operationId": "echo",
							"summary": "Echo",
							"requestBody": {
								"required": true,
								"content": {"application/json": {"schema": {"type": "object"}}}
							},
							"responses": {"200": {"description": "OK"}}
						}
					}
				}
			}`))
			return
		}
		if r.URL.Path == "/echo" && r.Method == "POST" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"message": "hello"}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer apiSrv.Close()

	// Replace the placeholder URL
	spec := `{
		"openapi": "3.0.3",
		"info": {"title": "Test", "version": "1.0.0"},
		"servers": [{"url": "` + apiSrv.URL + `"}],
		"paths": {
			"/echo": {
				"post": {
					"operationId": "echo",
					"summary": "Echo",
					"requestBody": {
						"required": true,
						"content": {"application/json": {"schema": {"type": "object"}}}
					},
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`
	specSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	defer specSrv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    specSrv.URL + "/openapi.json",
		BaseURL: apiSrv.URL,
	})

	err := client.Initialize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := client.RefreshTools(context.Background()); err != nil {
		t.Fatalf("unexpected error on RefreshTools: %v", err)
	}

	result, err := client.CallTool(context.Background(), "echo", map[string]any{
		"body": map[string]any{"msg": "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Error("expected successful tool call")
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content in result")
	}
}

func TestApplyAuth_Bearer(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:      "http://example.com/spec.json",
		BaseURL:   "http://example.com",
		AuthType:  "bearer",
		AuthToken: "my-token",
	})

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	if err := c.applyAuth(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.Get("Authorization") != "Bearer my-token" {
		t.Errorf("expected 'Bearer my-token', got %q", req.Header.Get("Authorization"))
	}
}

func TestApplyAuth_Header(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:       "http://example.com/spec.json",
		BaseURL:    "http://example.com",
		AuthType:   "header",
		AuthHeader: "X-API-Key",
		AuthValue:  "secret123",
	})

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	if err := c.applyAuth(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.Get("X-API-Key") != "secret123" {
		t.Errorf("expected 'secret123', got %q", req.Header.Get("X-API-Key"))
	}
}

func TestApplyAuth_NoAuth(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    "http://example.com/spec.json",
		BaseURL: "http://example.com",
	})

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	if err := c.applyAuth(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.Get("Authorization") != "" {
		t.Error("expected no Authorization header")
	}
}

func TestApplyAuth_Query(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:           "http://example.com/spec.json",
		BaseURL:        "http://example.com",
		AuthType:       "query",
		AuthQueryParam: "api_key",
		AuthQueryValue: "myapikey",
	})

	req, _ := http.NewRequest("GET", "http://example.com/test?existing=1", nil)
	if err := c.applyAuth(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	q := req.URL.Query()
	if q.Get("api_key") != "myapikey" {
		t.Errorf("expected api_key=myapikey, got %q", q.Get("api_key"))
	}
	// Existing query params must be preserved
	if q.Get("existing") != "1" {
		t.Errorf("existing query param lost; got %q", q.Get("existing"))
	}
}

func TestApplyAuth_Basic(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:          "http://example.com/spec.json",
		BaseURL:       "http://example.com",
		AuthType:      "basic",
		BasicUsername: "alice",
		BasicPassword: "s3cr3t",
	})

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	if err := c.applyAuth(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	user, pass, ok := req.BasicAuth()
	if !ok {
		t.Fatal("expected Basic auth header")
	}
	if user != "alice" || pass != "s3cr3t" {
		t.Errorf("expected alice/s3cr3t, got %q/%q", user, pass)
	}
}

func TestApplyAuth_OAuth2(t *testing.T) {
	// Mock token endpoint
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"test-oauth-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	c, err := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:               "http://example.com/spec.json",
		BaseURL:            "http://example.com",
		AuthType:           "oauth2",
		OAuth2ClientID:     "client-id",
		OAuth2ClientSecret: "client-secret",
		OAuth2TokenURL:     tokenServer.URL + "/token",
	})
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	req, _ := http.NewRequest("GET", "http://example.com/test", nil)
	if err := c.applyAuth(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if req.Header.Get("Authorization") != "Bearer test-oauth-token" {
		t.Errorf("expected 'Bearer test-oauth-token', got %q", req.Header.Get("Authorization"))
	}
}

func TestRefreshTools(t *testing.T) {
	spec := `{
		"openapi": "3.0.3",
		"info": {"title": "Test", "version": "1.0.0"},
		"servers": [{"url": "http://localhost"}],
		"paths": {
			"/test": {
				"get": {
					"operationId": "test",
					"summary": "Test",
					"responses": {"200": {"description": "OK"}}
				}
			}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spec))
	}))
	defer srv.Close()

	client, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/openapi.json",
		BaseURL: srv.URL,
	})

	err := client.Initialize(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// RefreshTools should re-parse the cached doc
	err = client.RefreshTools(context.Background())
	if err != nil {
		t.Fatalf("unexpected error on refresh: %v", err)
	}

	tools := client.Tools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool after refresh, got %d", len(tools))
	}
}

func TestPing_NoBaseURL(t *testing.T) {
	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec: "http://example.com/spec.json",
	})
	err := c.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no base URL") {
		t.Errorf("expected 'no base URL' error, got %v", err)
	}
}

func TestPing_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/spec.json",
		BaseURL: srv.URL,
	})
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPing_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:    srv.URL + "/spec.json",
		BaseURL: srv.URL,
	})
	if err := c.Ping(context.Background()); err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestPing_WithBearerAuth(t *testing.T) {
	const token = "ping-test-token"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:      srv.URL + "/spec.json",
		BaseURL:   srv.URL,
		AuthType:  "bearer",
		AuthToken: token,
	})
	if err := c.Ping(context.Background()); err != nil {
		t.Errorf("unexpected error with valid bearer token: %v", err)
	}
}

func TestNewOpenAPIClient_TLSInvalidCert(t *testing.T) {
	dir := t.TempDir()
	certFile := dir + "/cert.pem"
	keyFile := dir + "/key.pem"
	if err := os.WriteFile(certFile, []byte("not a cert"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte("not a key"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := NewOpenAPIClient("test", &OpenAPIClientConfig{
		Spec:        "http://example.com/spec.json",
		TLSCertFile: certFile,
		TLSKeyFile:  keyFile,
	})
	if err == nil || !strings.Contains(err.Error(), "loading TLS client certificate") {
		t.Errorf("expected TLS cert error, got %v", err)
	}
}
