package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

func TestHandleSkillSourceAdd_NoActivateSkills(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantCode   int
		wantStates map[string]registry.ItemState
		wantErr    string
	}{
		{
			name:     "listed skill imports as draft",
			body:     `"selected":["skill-a","skill-b"],"noActivateSkills":["skill-b"]`,
			wantCode: http.StatusCreated,
			wantStates: map[string]registry.ItemState{
				"skill-a": registry.StateActive,
				"skill-b": registry.StateDraft,
			},
		},
		{
			name:     "name outside selected is rejected before import",
			body:     `"selected":["skill-a"],"noActivateSkills":["skill-b"]`,
			wantCode: http.StatusBadRequest,
			wantErr:  "skill-b",
		},
		{
			name:     "absent field keeps every skill active",
			body:     `"selected":["skill-a","skill-b"]`,
			wantCode: http.StatusCreated,
			wantStates: map[string]registry.ItemState{
				"skill-a": registry.StateActive,
				"skill-b": registry.StateActive,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("GRIDCTL_HOME", home)
			srv, reg := setupRegistryTestServer(t)

			root := filepath.Join(home, "src", "pair")
			requireMkdirSkillNamed(t, filepath.Join(root, "skill-a"), "skill-a")
			requireMkdirSkillNamed(t, filepath.Join(root, "skill-b"), "skill-b")

			req := loopbackRequest(http.MethodPost, "/api/skills/sources",
				strings.NewReader(`{"repo":"`+root+`",`+tt.body+`}`))
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantCode, rec.Body.String())
			}
			if tt.wantErr != "" && !strings.Contains(rec.Body.String(), tt.wantErr) {
				t.Fatalf("error body %q does not name %q", rec.Body.String(), tt.wantErr)
			}
			if tt.wantCode != http.StatusCreated {
				if got := reg.Store().ListSkills(); len(got) != 0 {
					t.Fatalf("rejected request imported %d skills", len(got))
				}
				return
			}
			for name, want := range tt.wantStates {
				sk, err := reg.Store().GetSkill(name)
				if err != nil {
					t.Fatalf("get %s: %v", name, err)
				}
				if sk.State != want {
					t.Errorf("%s state = %q, want %q", name, sk.State, want)
				}
			}
		})
	}
}
