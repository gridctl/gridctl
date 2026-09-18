package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

func TestCardPins_IndependentStoreAndApproval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	gw := mcp.NewGateway()
	server := NewServer(gw, nil)
	store := pins.NewWithPath(t.TempDir(), "cards")
	if err := gw.SetCardPinStorage(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	server.SetCardPinStore(store)
	if _, err := store.VerifyOrPin("ordinary", []mcp.Tool{{Name: "_agent_card", Description: "ordinary callable tool"}}); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, loopbackRequest(method, path, strings.NewReader(body)))
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	// Ordinary sources keep the legacy-disabled list and get behavior.
	if w := request(http.MethodGet, "/api/pins", "", http.StatusOK); strings.TrimSpace(w.Body.String()) != "{}" {
		t.Fatal("disabled legacy pins leaked into inventory")
	}
	request(http.MethodGet, "/api/pins/ordinary", "", http.StatusServiceUnavailable)
	if server.hasCardPins("ordinary", store) {
		t.Fatal("ordinary tool name misclassified as mandatory trust")
	}
	makeSnapshot := func(card string) mcp.PinSnapshot {
		s, err := pins.NewCardSnapshot(1, pins.CardIdentity{Card: "https://agent.example/card"}, []byte(card), []mcp.Tool{{Name: "send"}})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	first, changed := makeSnapshot("first"), makeSnapshot("changed")
	ctx := context.Background()
	if _, err := gw.CardTrust().Register(ctx, "agent", first, func(context.Context) (mcp.PinSnapshot, error) { return changed, nil }, func() {}); err != nil {
		t.Fatal(err)
	}
	if err := gw.CardTrust().Observe(ctx, "agent", changed); err == nil {
		t.Fatal("drift accepted")
	}
	w := request(http.MethodGet, "/api/pins/agent/diff", "", http.StatusOK)
	var diff pinsDiffResponse
	if err := json.Unmarshal(w.Body.Bytes(), &diff); err != nil {
		t.Fatal(err)
	}
	if diff.LiveServerHash != changed.Hash() || len(diff.ModifiedTools) != 1 || diff.ModifiedTools[0].Name != "_agent_card" {
		t.Fatalf("immutable diff: %+v", diff)
	}
	request(http.MethodGet, "/api/pins/agent", "", http.StatusOK)
	request(http.MethodPost, "/api/pins/agent/approve", "", http.StatusBadRequest)
	request(http.MethodPost, "/api/pins/agent/approve", `{"expected_server_hash":"`+first.Hash()+`"}`, http.StatusConflict)
	request(http.MethodDelete, "/api/pins/agent", "", http.StatusConflict)
	request(http.MethodPost, "/api/pins/agent/approve", `{"expected_server_hash":"`+changed.Hash()+`"}`, http.StatusOK)
	if _, err := gw.CardTrust().Approved(ctx, "agent", 1); err != nil {
		t.Fatal(err)
	}
	w = request(http.MethodGet, "/api/pins", "", http.StatusOK)
	if strings.Contains(w.Body.String(), "ordinary") || !strings.Contains(w.Body.String(), "_agent_card") {
		t.Fatal("mandatory inventory missing or legacy inventory exposed")
	}
	if len(gw.Router().AggregatedTools()) != 0 {
		t.Fatal("hidden snapshot records became callable")
	}
}
