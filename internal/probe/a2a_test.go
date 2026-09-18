package probe

import (
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUnsupportedReason_A2A(t *testing.T) {
	err := unsupportedReason(config.MCPServer{A2A: &config.A2AConfig{Card: "http://127.0.0.1:1/card"}})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "A2A")
}
