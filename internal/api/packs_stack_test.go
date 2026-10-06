package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/packops"
)

func TestHandlePackApply_StackSkipsWithoutLauncher(t *testing.T) {
	srv, _ := setupPackTestServer(t)
	manifest := `apiVersion: gridctl.dev/v1
kind: Pack
name: team-pack
skills: [alpha]
wiring: true
stack: stack.yaml
`
	stack := "version: \"1\"\nname: team-pack\nmcp-servers:\n  - name: local\n    command: [\"echo\"]\n"
	repo := packRepoFixture(t, manifest, map[string]string{"stack.yaml": stack})
	if rec := doJSON(t, srv, http.MethodPost, "/api/packs", `{"repo":`+jsonQuote(repo)+`}`); rec.Code != http.StatusCreated {
		t.Fatalf("add = %d: %s", rec.Code, rec.Body.String())
	}

	preview := doJSON(t, srv, http.MethodPost, "/api/packs/preview", `{"repo":`+jsonQuote(repo)+`}`)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"stack"`) {
		t.Fatalf("preview = %d: %s", preview.Code, preview.Body.String())
	}

	rec := doJSON(t, srv, http.MethodPost, "/api/packs/team-pack/apply", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("apply = %d: %s", rec.Code, rec.Body.String())
	}
	var doc packops.ApplyDoc
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Rows) == 0 || doc.Rows[0].Kind != "stack" || doc.Rows[0].Action != "skipped-unavailable" {
		t.Fatalf("rows = %+v", doc.Rows)
	}
	if !strings.Contains(doc.Rows[0].Remediation, "gridctl pack apply team-pack") {
		t.Fatalf("remediation = %q", doc.Rows[0].Remediation)
	}
	var wiring *packops.Row
	for i := range doc.Rows {
		if doc.Rows[i].Kind == "wiring" {
			wiring = &doc.Rows[i]
			break
		}
	}
	if wiring == nil || wiring.Action != "skipped-unavailable" || !strings.Contains(wiring.Detail, "pack stack is not running") {
		t.Fatalf("wiring = %+v", wiring)
	}
}
