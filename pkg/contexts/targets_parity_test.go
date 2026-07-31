package contexts_test

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/contexts"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// TestContextSlugsMatchProvisionerRegistry guards the deliberate copy of
// client slugs in the context target table (following
// pkg/config/links_parity_test.go). Both supported and
// deliberately-unsupported clients must use slugs the provisioner
// registry knows, so all gridctl surfaces speak one client-identifier
// language.
func TestContextSlugsMatchProvisionerRegistry(t *testing.T) {
	registry := provisioner.NewRegistry()

	for _, tgt := range contexts.Targets() {
		if _, ok := registry.FindBySlug(tgt.Slug); !ok {
			t.Errorf("context target slug %q is unknown to the provisioner registry", tgt.Slug)
		}
	}
	for _, u := range contexts.Unsupported() {
		if _, ok := registry.FindBySlug(u.Slug); !ok {
			t.Errorf("unsupported-client slug %q is unknown to the provisioner registry", u.Slug)
		}
	}
}
