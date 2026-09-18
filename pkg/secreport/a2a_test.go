package secreport

import (
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestServerKind_A2A(t *testing.T) {
	require.Equal(t, "a2a", serverKind(config.MCPServer{A2A: &config.A2AConfig{Card: "https://example.com/card"}}))
}
