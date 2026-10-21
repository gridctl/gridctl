package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
	"github.com/gridctl/gridctl/pkg/skillsext"
)

func TestHandleStatus_SkillsExtension(t *testing.T) {
	srv, reg := setupRegistryTestServer(t)
	srv.gateway.SetSkillProvider(skillsext.New(reg.Store()))
	for _, populated := range []bool{false, true, false} {
		if populated {
			seedSkill(t, reg, "alpha", registry.StateActive)
			srv.gateway.Router().AddClient(reg)
		} else {
			srv.gateway.Router().RemoveClient("registry")
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/status", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status: %s", rec.Body.String())
		}
		var result map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if populated {
			var extensions []string
			if err := json.Unmarshal(result["extensions"], &extensions); err != nil {
				t.Fatal(err)
			}
			if len(extensions) != 1 || extensions[0] != mcp.SkillsExtensionID {
				t.Fatalf("extensions: %v", extensions)
			}
		} else if _, exists := result["extensions"]; exists {
			t.Fatal("empty registry advertised skills")
		}
	}
}
