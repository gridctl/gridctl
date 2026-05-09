package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestNewToolCaller_NilGateway(t *testing.T) {
	t.Parallel()

	caller, err := NewToolCaller(nil)
	if !errors.Is(err, ErrNilGateway) {
		t.Errorf("err = %v, want ErrNilGateway", err)
	}
	if caller != nil {
		t.Errorf("caller = %v, want nil", caller)
	}
}

func TestNewToolCaller_DelegatesThroughGateway(t *testing.T) {
	t.Parallel()

	gw := mcp.NewGateway()
	defer gw.Close()

	caller, err := NewToolCaller(gw)
	if err != nil {
		t.Fatalf("NewToolCaller: %v", err)
	}
	if caller == nil {
		t.Fatal("caller is nil with non-nil gateway")
	}

	// An empty gateway has no tools registered, so any CallTool
	// surfaces a tool-result with IsError=true — the goal is to
	// confirm the call reaches the gateway, not to test routing.
	res, err := caller.CallTool(context.Background(), "nonexistent", map[string]any{})
	if err != nil {
		// A direct error path is also valid — different gateway
		// states produce different shapes — we just want either an
		// err or an IsError=true result, not a quietly empty success.
		return
	}
	if res == nil {
		t.Fatal("nil result and nil error from CallTool")
	}
	if !res.IsError {
		t.Errorf("CallTool against empty gateway should mark IsError=true; got %+v", res)
	}
}
