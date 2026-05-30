package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gridctl/gridctl/pkg/mcp"
)

func TestImpactFromScopes_LosesAndGains(t *testing.T) {
	before := mcp.ClientScopeResult{
		Configured: true,
		Servers:    []string{"github", "gitlab"},
		Tools:      []string{"github__a", "github__b", "gitlab__c"},
	}
	after := mcp.ClientScopeResult{
		Configured: true,
		Servers:    []string{"github", "slack"},
		Tools:      []string{"github__a", "github__b", "slack__d"},
	}
	imp := impactFromScopes("Cursor", "cursor", before, after)

	assert.Equal(t, "Cursor", imp.Name)
	assert.Equal(t, "cursor", imp.Slug)
	assert.Equal(t, 2, imp.BeforeServers)
	assert.Equal(t, 2, imp.AfterServers)
	assert.Equal(t, 3, imp.BeforeTools)
	assert.Equal(t, 3, imp.AfterTools)
	assert.Equal(t, []string{"gitlab"}, imp.LostServers)
	assert.Equal(t, []string{"slack"}, imp.GainedServers)
}

func TestImpactFromScopes_FullLockout(t *testing.T) {
	before := mcp.ClientScopeResult{Configured: true, Servers: []string{"github"}, Tools: []string{"github__a"}}
	after := mcp.ClientScopeResult{Configured: true} // reaches nothing
	imp := impactFromScopes("Bot", "bot", before, after)

	assert.Equal(t, 0, imp.AfterServers)
	assert.Equal(t, 0, imp.AfterTools)
	assert.Equal(t, []string{"github"}, imp.LostServers)
	assert.Empty(t, imp.GainedServers)
}

func TestDerefStringsNil_PreservesNil(t *testing.T) {
	assert.Nil(t, derefStringsNil(nil))
	got := derefStringsNil(sp("a", "b"))
	assert.Equal(t, []string{"a", "b"}, got)
}
