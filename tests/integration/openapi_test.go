package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Test OpenAPI spec served by the mock server.
const testOpenAPISpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Test API",
    "version": "1.0.0",
    "description": "A simple test API for integration testing"
  },
  "servers": [
    {
      "url": "SERVER_URL_PLACEHOLDER"
    }
  ],
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List all items",
        "description": "Returns a list of all items in the store",
        "parameters": [
          {
            "name": "limit",
            "in": "query",
            "description": "Maximum number of items to return",
            "required": false,
            "schema": {
              "type": "integer",
              "default": 10
            }
          }
        ],
        "responses": {
          "200": {
            "description": "A list of items",
            "content": {
              "application/json": {
                "schema": {
                  "type": "array",
                  "items": {
                    "$ref": "#/components/schemas/Item"
                  }
                }
              }
            }
          }
        }
      },
      "post": {
        "operationId": "createItem",
        "summary": "Create a new item",
        "description": "Creates a new item in the store",
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/NewItem"
              }
            }
          }
        },
        "responses": {
          "201": {
            "description": "Item created successfully",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Item"
                }
              }
            }
          }
        }
      }
    },
    "/items/{id}": {
      "get": {
        "operationId": "getItem",
        "summary": "Get an item by ID",
        "description": "Returns a single item by its ID",
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "description": "The item ID",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "200": {
            "description": "The requested item",
            "content": {
              "application/json": {
                "schema": {
                  "$ref": "#/components/schemas/Item"
                }
              }
            }
          },
          "404": {
            "description": "Item not found"
          }
        }
      },
      "delete": {
        "operationId": "deleteItem",
        "summary": "Delete an item",
        "description": "Deletes an item by its ID",
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "description": "The item ID",
            "required": true,
            "schema": {
              "type": "string"
            }
          }
        ],
        "responses": {
          "204": {
            "description": "Item deleted successfully"
          },
          "404": {
            "description": "Item not found"
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "Item": {
        "type": "object",
        "properties": {
          "id": {
            "type": "string"
          },
          "name": {
            "type": "string"
          },
          "price": {
            "type": "number"
          }
        },
        "required": ["id", "name"]
      },
      "NewItem": {
        "type": "object",
        "properties": {
          "name": {
            "type": "string"
          },
          "price": {
            "type": "number"
          }
        },
        "required": ["name"]
      }
    }
  }
}`

// Mock data for the test server.
var mockItems = []map[string]interface{}{
	{"id": "1", "name": "Widget", "price": 9.99},
	{"id": "2", "name": "Gadget", "price": 19.99},
	{"id": "3", "name": "Gizmo", "price": 29.99},
}

// createTestServer creates an httptest server that serves the OpenAPI spec
// and implements the API endpoints defined in it.
func createTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Serve the OpenAPI spec
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testOpenAPISpec))
	})

	// GET /items - list all items
	mux.HandleFunc("GET /items", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(mockItems)
	})

	// POST /items - create a new item
	mux.HandleFunc("POST /items", func(w http.ResponseWriter, r *http.Request) {
		var newItem map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&newItem); err != nil {
			http.Error(w, `{"error": "invalid JSON"}`, http.StatusBadRequest)
			return
		}

		// Assign a new ID
		newItem["id"] = "4"

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(newItem)
	})

	// GET /items/{id} - get item by ID
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		for _, item := range mockItems {
			if item["id"] == id {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(item)
				return
			}
		}

		http.Error(w, `{"error": "item not found"}`, http.StatusNotFound)
	})

	// DELETE /items/{id} - delete item by ID
	mux.HandleFunc("DELETE /items/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		for _, item := range mockItems {
			if item["id"] == id {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}

		http.Error(w, `{"error": "item not found"}`, http.StatusNotFound)
	})

	return httptest.NewServer(mux)
}

// createTestServerWithAuth creates a test server that requires Bearer token auth.
func createTestServerWithAuth(t *testing.T, expectedToken string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// Auth middleware
	authMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			// Skip auth for the spec endpoint
			if r.URL.Path == "/openapi.json" {
				next(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+expectedToken {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}

	// Serve the OpenAPI spec (no auth required)
	mux.HandleFunc("/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(testOpenAPISpec))
	})

	// GET /items with auth
	mux.HandleFunc("GET /items", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(mockItems)
	}))

	return httptest.NewServer(mux)
}

func TestOpenAPIClient_RefreshTools(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	// Initialize the client
	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Refresh tools
	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Get tools after refresh
	tools := client.Tools()

	// Verify we got the expected tools
	expectedTools := map[string]bool{
		"listItems":  false,
		"createItem": false,
		"getItem":    false,
		"deleteItem": false,
	}

	for _, tool := range tools {
		if _, exists := expectedTools[tool.Name]; exists {
			expectedTools[tool.Name] = true
		} else {
			t.Errorf("Unexpected tool: %s", tool.Name)
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("Expected tool not found: %s", name)
		}
	}

	// Verify tool count
	if len(tools) != 4 {
		t.Errorf("Expected 4 tools, got %d", len(tools))
	}
}

func TestOpenAPIClient_RefreshTools_WithIncludeFilter(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
		Include: []string{"listItems", "getItem"},
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	tools := client.Tools()

	// Should only have the included tools
	if len(tools) != 2 {
		t.Errorf("Expected 2 tools, got %d", len(tools))
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	if !toolNames["listItems"] {
		t.Error("Expected listItems tool to be included")
	}
	if !toolNames["getItem"] {
		t.Error("Expected getItem tool to be included")
	}
	if toolNames["createItem"] {
		t.Error("createItem should not be included")
	}
	if toolNames["deleteItem"] {
		t.Error("deleteItem should not be included")
	}
}

func TestOpenAPIClient_RefreshTools_WithExcludeFilter(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
		Exclude: []string{"deleteItem"},
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	tools := client.Tools()

	// Should have 3 tools (all except deleteItem)
	if len(tools) != 3 {
		t.Errorf("Expected 3 tools, got %d", len(tools))
	}

	for _, tool := range tools {
		if tool.Name == "deleteItem" {
			t.Error("deleteItem should be excluded")
		}
	}
}

func TestOpenAPIClient_CallTool_ListItems(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Refresh tools first to populate the operations
	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call the listItems tool
	result, err := client.CallTool(ctx, "listItems", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	// Check the result
	if result.IsError {
		t.Errorf("Expected successful result, got error: %v", result.Content)
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}

	// Parse the response content
	var items []map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &items); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(items))
	}
}

func TestOpenAPIClient_CallTool_GetItemById(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call getItem with path parameter
	result, err := client.CallTool(ctx, "getItem", map[string]interface{}{
		"id": "2",
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected successful result, got error")
	}

	var item map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &item); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if item["id"] != "2" {
		t.Errorf("Expected id '2', got '%v'", item["id"])
	}
	if item["name"] != "Gadget" {
		t.Errorf("Expected name 'Gadget', got '%v'", item["name"])
	}
}

func TestOpenAPIClient_CallTool_CreateItem(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call createItem with request body
	result, err := client.CallTool(ctx, "createItem", map[string]interface{}{
		"body": map[string]interface{}{
			"name":  "New Item",
			"price": 39.99,
		},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected successful result, got error")
	}

	var item map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content[0].Text), &item); err != nil {
		t.Fatalf("Failed to parse response JSON: %v", err)
	}

	if item["name"] != "New Item" {
		t.Errorf("Expected name 'New Item', got '%v'", item["name"])
	}
	if item["id"] != "4" {
		t.Errorf("Expected id '4', got '%v'", item["id"])
	}
}

func TestOpenAPIClient_CallTool_DeleteItem(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call deleteItem
	result, err := client.CallTool(ctx, "deleteItem", map[string]interface{}{
		"id": "1",
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected successful result, got error")
	}
}

func TestOpenAPIClient_CallTool_NotFound(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call getItem with non-existent ID
	result, err := client.CallTool(ctx, "getItem", map[string]interface{}{
		"id": "999",
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	// HTTP 404 should result in isError=true
	if !result.IsError {
		t.Error("Expected isError=true for 404 response")
	}
}

func TestOpenAPIClient_CallTool_MissingPathParam(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call getItem without the required 'id' parameter
	result, err := client.CallTool(ctx, "getItem", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	// Should return an error about missing path parameter
	if !result.IsError {
		t.Error("Expected isError=true for missing path parameter")
	}

	if len(result.Content) == 0 {
		t.Fatal("Expected error content")
	}

	if !strings.Contains(result.Content[0].Text, "id") {
		t.Errorf("Expected error message to mention 'id' parameter, got: %s", result.Content[0].Text)
	}
}

func TestOpenAPIClient_CallTool_UnknownTool(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call a non-existent tool
	result, err := client.CallTool(ctx, "nonExistentTool", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if !result.IsError {
		t.Error("Expected isError=true for unknown tool")
	}
}

func TestOpenAPIClient_WithBearerAuth(t *testing.T) {
	expectedToken := "test-secret-token"
	server := createTestServerWithAuth(t, expectedToken)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:      server.URL + "/openapi.json",
		BaseURL:   server.URL,
		AuthType:  "bearer",
		AuthToken: expectedToken,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call should succeed with correct auth
	result, err := client.CallTool(ctx, "listItems", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if result.IsError {
		t.Errorf("Expected successful result with valid auth, got error")
	}
}

func TestOpenAPIClient_WithBearerAuth_InvalidToken(t *testing.T) {
	expectedToken := "test-secret-token"
	server := createTestServerWithAuth(t, expectedToken)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:      server.URL + "/openapi.json",
		BaseURL:   server.URL,
		AuthType:  "bearer",
		AuthToken: "wrong-token",
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	// Call should fail with incorrect auth
	result, err := client.CallTool(ctx, "listItems", map[string]interface{}{})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}

	if !result.IsError {
		t.Error("Expected isError=true for 401 response")
	}
}

func TestOpenAPIClient_ToolSchema(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	tools := client.Tools()

	// Find the getItem tool and verify its schema includes the required 'id' parameter
	for _, tool := range tools {
		if tool.Name == "getItem" {
			// InputSchema is json.RawMessage, unmarshal it
			var schema map[string]interface{}
			if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
				t.Fatalf("Failed to unmarshal InputSchema: %v", err)
			}

			properties, ok := schema["properties"].(map[string]interface{})
			if !ok {
				t.Fatal("Expected properties in schema")
			}

			if _, hasID := properties["id"]; !hasID {
				t.Error("Expected 'id' property in getItem schema")
			}

			// Check that 'id' is marked as required
			required, ok := schema["required"].([]interface{})
			if !ok {
				t.Fatal("Expected required array in schema")
			}

			foundRequired := false
			for _, r := range required {
				if r == "id" {
					foundRequired = true
					break
				}
			}
			if !foundRequired {
				t.Error("Expected 'id' to be in required list")
			}

			return
		}
	}

	t.Error("getItem tool not found")
}

func TestOpenAPIClient_ToolDescription(t *testing.T) {
	server := createTestServer(t)
	defer server.Close()

	cfg := &mcp.OpenAPIClientConfig{
		Spec:    server.URL + "/openapi.json",
		BaseURL: server.URL,
	}

	client, err := mcp.NewOpenAPIClient("test-api", cfg)
	if err != nil {
		t.Fatalf("NewOpenAPIClient failed: %v", err)
	}

	ctx := context.Background()

	if err := client.Initialize(ctx); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if err := client.RefreshTools(ctx); err != nil {
		t.Fatalf("RefreshTools failed: %v", err)
	}

	tools := client.Tools()

	// Check that tools have descriptions
	for _, tool := range tools {
		if tool.Name == "listItems" {
			if tool.Description == "" {
				t.Error("Expected listItems to have a description")
			}
			// The description should include either summary or description from spec
			if !strings.Contains(tool.Description, "List") && !strings.Contains(tool.Description, "items") {
				t.Errorf("Expected description to contain 'List' or 'items', got: %s", tool.Description)
			}
			return
		}
	}

	t.Error("listItems tool not found")
}

func TestOpenAPIClient_EnvVarExpansion(t *testing.T) {
	// Create a mock server to handle API requests
	server := createTestServer(t)
	defer server.Close()

	// Create a temp spec file with environment variable placeholders
	specContent := `{
  "openapi": "3.0.3",
  "info": {"title": "Test API", "version": "1.0.0"},
  "servers": [{"url": "${TEST_OPENAPI_BASE_URL:-http://localhost:8080}"}],
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List all items",
        "responses": {"200": {"description": "A list of items"}}
      }
    }
  }
}`

	dir := t.TempDir()
	specPath := dir + "/spec.json"
	if err := os.WriteFile(specPath, []byte(specContent), 0644); err != nil {
		t.Fatalf("failed to write temp spec: %v", err)
	}

	t.Run("expansion with default value", func(t *testing.T) {
		// Ensure env var is not set - should use default
		os.Unsetenv("TEST_OPENAPI_BASE_URL")

		cfg := &mcp.OpenAPIClientConfig{
			Spec:    specPath,
			BaseURL: "", // Let it come from spec
		}

		client, err := mcp.NewOpenAPIClient("test-api", cfg)
		if err != nil {
			t.Fatalf("NewOpenAPIClient failed: %v", err)
		}

		ctx := context.Background()
		if err := client.Initialize(ctx); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		// Check that the default URL was used
		info := client.ServerInfo()
		if info.Name != "Test API" {
			t.Errorf("Expected server name 'Test API', got '%s'", info.Name)
		}
	})

	t.Run("expansion with env var set", func(t *testing.T) {
		// Set env var to the mock server URL
		os.Setenv("TEST_OPENAPI_BASE_URL", server.URL)
		defer os.Unsetenv("TEST_OPENAPI_BASE_URL")

		cfg := &mcp.OpenAPIClientConfig{
			Spec: specPath,
		}

		client, err := mcp.NewOpenAPIClient("test-api", cfg)
		if err != nil {
			t.Fatalf("NewOpenAPIClient failed: %v", err)
		}

		ctx := context.Background()
		if err := client.Initialize(ctx); err != nil {
			t.Fatalf("Initialize failed: %v", err)
		}

		if err := client.RefreshTools(ctx); err != nil {
			t.Fatalf("RefreshTools failed: %v", err)
		}

		// Call should work because the URL points to our mock server
		result, err := client.CallTool(ctx, "listItems", map[string]interface{}{})
		if err != nil {
			t.Fatalf("CallTool failed: %v", err)
		}

		if result.IsError {
			t.Errorf("Expected successful result, got error: %v", result.Content)
		}
	})

	t.Run("no expansion with NoExpand flag", func(t *testing.T) {
		// Set env var but disable expansion
		os.Setenv("TEST_OPENAPI_BASE_URL", server.URL)
		defer os.Unsetenv("TEST_OPENAPI_BASE_URL")

		cfg := &mcp.OpenAPIClientConfig{
			Spec:     specPath,
			NoExpand: true,
		}

		client, err := mcp.NewOpenAPIClient("test-api", cfg)
		if err != nil {
			t.Fatalf("NewOpenAPIClient failed: %v", err)
		}

		ctx := context.Background()
		// Initialize should fail because the literal ${...} is not a valid URL
		err = client.Initialize(ctx)
		if err == nil {
			t.Error("Expected initialization to fail with unexpanded variable, but it succeeded")
		}
	})
}
