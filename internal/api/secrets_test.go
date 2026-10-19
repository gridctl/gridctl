package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/reload"
)

func TestHandleSecrets_EmptyWithoutStack(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/secrets", nil)
	rec := httptest.NewRecorder()
	s.handleSecrets(rec, req)
	var body secretsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Providers == nil || body.References == nil || len(body.Providers) != 0 || len(body.References) != 0 {
		t.Fatalf("body = %#v", body)
	}
}

func TestBuildSecretsResponse_NoValues(t *testing.T) {
	spec := &config.Stack{
		Secrets: &config.Secrets{Providers: []config.SecretProvider{{
			Name: "op", Type: "onepassword", TokenFile: "/tmp/op", Timeout: "10s",
		}}},
		SecretRefs: []config.SecretRef{{Provider: "op", Path: "a/b", Key: "secret:op/a/b"}},
		References: config.ReferenceIndex{
			"secret:op/a/b": {{Kind: config.RefKindMCPServer, Name: "netbox", Field: "env.TOKEN"}},
		},
	}
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	body := buildSecretsResponse(spec, reload.SecretObservation{
		Resolved:   map[string]string{"secret:op/a/b": "super-secret-value"},
		ResolvedAt: map[string]time.Time{"secret:op/a/b": at},
	})
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret-value") || strings.Contains(string(raw), "/tmp/op") {
		t.Fatalf("response leaked: %s", raw)
	}
	if body.References[0].Status != "resolved" || body.Providers[0].TokenFile != true {
		t.Fatalf("body = %#v", body)
	}
	if body.References[0].LastResolved == nil || *body.References[0].LastResolved != at.Format(time.RFC3339) {
		t.Fatalf("last_resolved = %#v", body.References[0].LastResolved)
	}
}
