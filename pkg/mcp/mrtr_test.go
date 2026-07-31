package mcp

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"
)

// registerMRTRServer wires a mock server whose tool returns an MRTR
// interim result carrying origin requestState.
func registerMRTRServer(t *testing.T, g *Gateway, ctrl *gomock.Controller, name, originState string) {
	t.Helper()
	client := setupMockAgentClient(ctrl, name, []Tool{{Name: "ask"}})
	client.EXPECT().CallTool(gomock.Any(), "ask", gomock.Any()).Return(&ToolCallResult{
		ResultType:   ResultTypeInputRequired,
		RequestState: originState,
	}, nil).AnyTimes()
	g.Router().AddClient(client)
	g.Router().RefreshTools()
}

func TestHandleToolsCall_WrapsMRTRRequestState(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	registerMRTRServer(t, g, ctrl, "srv", "AEAD-protected blob")

	result, err := g.HandleToolsCall(context.Background(), ToolCallParams{Name: "srv__ask", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultType != ResultTypeInputRequired {
		t.Fatalf("resultType = %q, want input_required", result.ResultType)
	}
	if result.RequestState == "AEAD-protected blob" {
		t.Fatal("requestState must be enveloped, not relayed bare")
	}
	server, origin, ok := unwrapRequestState(result.RequestState)
	if !ok || server != "srv" || origin != "AEAD-protected blob" {
		t.Fatalf("envelope round-trip failed: %q %q %v", server, origin, ok)
	}
}

func TestHandleToolsCall_MRTRRetryServerMismatchFailsLoud(t *testing.T) {
	ctrl := gomock.NewController(t)
	g := NewGateway()
	registerMRTRServer(t, g, ctrl, "srv", "state")

	ctx := withMRTRRelay(context.Background(), &mrtrRelay{ExpectedServer: "other-server", RequestState: "state"})
	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "srv__ask", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("mismatched MRTR retry must fail")
	}
	if !strings.Contains(result.Content[0].Text, "other-server") {
		t.Errorf("error should name the originating server: %+v", result.Content)
	}
}

func TestMissingInputCapability(t *testing.T) {
	inputRequests := []byte(`{
		"login": {"method": "elicitation/create", "params": {}},
		"question": {"method": "sampling/createMessage", "params": {}}
	}`)
	tests := []struct {
		name         string
		capabilities string
		want         string
	}{
		{"none declared", `{}`, "elicitation"},
		{"elicitation only", `{"elicitation":{}}`, "sampling"},
		{"all declared", `{"elicitation":{},"sampling":{}}`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := missingInputCapability(inputRequests, []byte(tc.capabilities))
			if got != tc.want {
				t.Errorf("missingInputCapability = %q, want %q", got, tc.want)
			}
		})
	}
}
