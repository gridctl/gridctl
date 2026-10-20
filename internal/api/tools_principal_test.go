package api

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/stretchr/testify/require"
)

func TestWithDeclaredClient_PrincipalPrecedence(t *testing.T) {
	const principal = "Alice+/Subject="
	for _, declared := range []string{"cursor", "cli", "other"} {
		ctx := withDeclaredClient(mcp.WithPrincipal(context.Background(), principal), declared)
		require.Equal(t, principal, mcp.ClientAccessIDFromContext(ctx))
		require.Equal(t, declared, mcp.ClientIDFromContext(ctx))
		legacy := withDeclaredClient(context.Background(), declared)
		require.Equal(t, declared, mcp.ClientAccessIDFromContext(legacy))
	}
}
