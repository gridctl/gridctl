package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
