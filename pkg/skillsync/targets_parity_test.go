package skillsync_test

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/provisioner"
	"github.com/gridctl/gridctl/pkg/skillsync"
)

// TestProjectionSlugsMatchProvisionerRegistry guards the deliberate copy
// of client slugs in the projection target table (following
// pkg/config/links_parity_test.go). Every slug must be one the
// provisioner registry knows, so all gridctl surfaces speak one
// client-identifier language. "agents" is the documented exception: it
// names the vendor-neutral ~/.agents/skills interop dir, which is
// multi-client by design and has no provisioner entry.
func TestProjectionSlugsMatchProvisionerRegistry(t *testing.T) {
	registry := provisioner.NewRegistry()
	exceptions := map[string]bool{"agents": true}

	for _, tgt := range skillsync.Targets() {
		if exceptions[tgt.Slug] {
			continue
		}
		if _, ok := registry.FindBySlug(tgt.Slug); !ok {
			t.Errorf("projection target slug %q is unknown to the provisioner registry", tgt.Slug)
		}
	}
}
