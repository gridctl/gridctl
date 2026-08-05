package agentsync_test

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/agentsync"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// slugParityExceptions are agent targets that deliberately do not map to
// a provisioner client. Every entry needs a reason: UI surfaces that
// join agent projections to the client list by slug must special-case
// these rather than expecting a client row to attach to.
var slugParityExceptions = map[string]string{
	// GitHub Copilot's global agents directory (~/.copilot/agents) is its
	// own product surface, read by Copilot across editors; the provisioner
	// registry's closest client is "vscode", whose MCP config is a
	// different file with different ownership. Renaming the target would
	// also break the shipped `--clients copilot` CLI vocabulary.
	"copilot": "global agents dir is a distinct surface from the vscode client config",
}

// TestAgentTargetSlugsMatchProvisionerRegistry guards the deliberate
// copy of client slugs in the agent target table (following
// pkg/contexts/targets_parity_test.go). A target slug the provisioner
// registry does not know is silent drift unless it is a documented
// exception above — the Connections hub and Library projection chips
// join on these slugs.
func TestAgentTargetSlugsMatchProvisionerRegistry(t *testing.T) {
	registry := provisioner.NewRegistry()

	for _, tgt := range agentsync.Targets() {
		if _, ok := registry.FindBySlug(tgt.Slug); ok {
			continue
		}
		if reason, excepted := slugParityExceptions[tgt.Slug]; excepted {
			t.Logf("agent target %q is a documented non-provisioner surface: %s", tgt.Slug, reason)
			continue
		}
		t.Errorf("agent target slug %q is unknown to the provisioner registry and not a documented exception", tgt.Slug)
	}
}
