package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/internal/openapipreview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const previewTestSpec = `
openapi: 3.0.0
info:
  title: Petstore
  version: "1.0.17"
paths:
  /pets:
    get:
      operationId: listPets
      summary: List pets
      tags: [pet]
    post:
      summary: No operation id
  /pets/{id}:
    delete:
      operationId: pets.delete
      summary: Dotted id
`

func previewSpecServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(previewTestSpec))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postPreview(t *testing.T, s *Server, body string) (int, openAPIPreviewResponse, map[string]map[string]string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/openapi/operations", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleOpenAPIPreview(rec, req)

	var ok openAPIPreviewResponse
	var errWire map[string]map[string]string
	raw := rec.Body.Bytes()
	if rec.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(raw, &ok))
	} else {
		require.NoError(t, json.Unmarshal(raw, &errWire))
	}
	return rec.Code, ok, errWire
}

func wiredPreviewServer() *Server {
	s := &Server{}
	s.SetOpenAPIPreviewer(openapipreview.New(openapipreview.NewCache(openapipreview.DefaultTTL), nil))
	return s
}

func TestHandleOpenAPIPreview_Unwired(t *testing.T) {
	code, _, errWire := postPreview(t, &Server{}, `{"spec":"https://example.com/spec.json"}`)

	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, openapipreview.CodeInternal, errWire["error"]["code"])
	assert.NotEmpty(t, errWire["error"]["hint"], "an unwired daemon should still tell the operator what to do")
}

func TestHandleOpenAPIPreview_ReturnsOperations(t *testing.T) {
	srv := previewSpecServer(t)
	code, body, _ := postPreview(t, wiredPreviewServer(), `{"spec":"`+srv.URL+`"}`)

	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "Petstore", body.Title)
	assert.Equal(t, "1.0.17", body.Version)
	require.Len(t, body.Operations, 3)
	assert.Equal(t, 1, body.SkippedCount)
	assert.NotEmpty(t, body.LoadedAt)
	assert.False(t, body.Cached)
}

// The wire must carry the raw operationId (what operations.include matches) and
// the sanitized tool name (what the model sees) separately.
func TestHandleOpenAPIPreview_ExposesRawAndSanitizedIDs(t *testing.T) {
	srv := previewSpecServer(t)
	_, body, _ := postPreview(t, wiredPreviewServer(), `{"spec":"`+srv.URL+`"}`)

	var found bool
	for _, op := range body.Operations {
		if op.Path == "/pets/{id}" {
			found = true
			assert.Equal(t, "pets.delete", op.OperationID, "raw id must survive to the wire")
			assert.Equal(t, "pets_delete", op.ToolName, "sanitized name must be reported alongside")
			assert.Equal(t, "DELETE", op.Method)
		}
	}
	assert.True(t, found, "dotted operation missing from the response")
}

func TestHandleOpenAPIPreview_SkippedOperationCarriesReason(t *testing.T) {
	srv := previewSpecServer(t)
	_, body, _ := postPreview(t, wiredPreviewServer(), `{"spec":"`+srv.URL+`"}`)

	var skipped *openAPIOperationWire
	for i := range body.Operations {
		if body.Operations[i].Skipped {
			skipped = &body.Operations[i]
		}
	}
	require.NotNil(t, skipped, "operations without an operationId must be reported, not dropped")
	assert.Equal(t, "no_operation_id", skipped.SkipReason)
	assert.Equal(t, "POST", skipped.Method)
	assert.Empty(t, skipped.OperationID)
}

func TestHandleOpenAPIPreview_InvalidJSON(t *testing.T) {
	code, _, errWire := postPreview(t, wiredPreviewServer(), `{not json`)

	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, openapipreview.CodeInvalidRequest, errWire["error"]["code"])
}

func TestHandleOpenAPIPreview_EmptySpec(t *testing.T) {
	code, _, errWire := postPreview(t, wiredPreviewServer(), `{"spec":""}`)

	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, openapipreview.CodeInvalidRequest, errWire["error"]["code"])
	assert.NotEmpty(t, errWire["error"]["hint"])
}

func TestHandleOpenAPIPreview_UnreachableSpecIsUnprocessable(t *testing.T) {
	code, _, errWire := postPreview(t, wiredPreviewServer(),
		`{"spec":"http://127.0.0.1:1/nope.json"}`)

	assert.Equal(t, http.StatusUnprocessableEntity, code)
	assert.NotEmpty(t, errWire["error"]["message"])
	assert.NotEmpty(t, errWire["error"]["hint"])
}

func TestHandleOpenAPIPreview_SecondCallIsCached(t *testing.T) {
	srv := previewSpecServer(t)
	s := wiredPreviewServer()

	_, first, _ := postPreview(t, s, `{"spec":"`+srv.URL+`"}`)
	assert.False(t, first.Cached)

	_, second, _ := postPreview(t, s, `{"spec":"`+srv.URL+`"}`)
	assert.True(t, second.Cached, "repeated loads of the same spec should hit the cache")
}
