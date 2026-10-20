package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrincipal_GateProfileResolution(t *testing.T) {
	g := NewGateway()
	g.SetClientAccessPolicy(NewClientAccessPolicy(&ClientAccessSpec{
		Default: "allow",
		Profiles: map[string]ClientProfileSpec{
			"alice": {Aliases: []string{"Subject+/="}},
			"bob":   {Aliases: []string{"subject+/="}},
		},
	}))
	g.SetCallGates([]CallGate{&stubGate{name: "allow", decision: GateAllow()}})
	for _, tc := range []struct{ principal, profile string }{
		{"Subject+/=", "alice"},
		{"subject+/=", "bob"},
		{"ALICE", "alice"},
		{"unknown+/=", ""},
	} {
		ctx := WithPrincipal(context.Background(), tc.principal)
		ctx = WithClientAccessID(WithClientID(ctx, "cursor"), tc.principal)
		call, _, _ := g.checkCallGates(ctx, "alpha__echo")
		require.Equal(t, tc.principal, call.ClientAccessID)
		require.Equal(t, tc.profile, call.ClientProfileKey)
	}
	call, _, _ := g.checkCallGates(WithClientAccessID(context.Background(), "Subject+/="), "alpha__echo")
	require.Empty(t, call.ClientProfileKey, "legacy gate identity is unchanged")
}
