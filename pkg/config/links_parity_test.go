package config_test

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/provisioner"
)

// TestLinkSlugsMatchProvisionerRegistry guards the deliberate copy of the
// provisioner slug set in pkg/config (config is foundational and does not
// import pkg/provisioner; see clientmodels_parity_test.go for the same
// pattern against pkg/mcp). If a client is added to or removed from the
// registry, this fails until links.go is updated.
func TestLinkSlugsMatchProvisionerRegistry(t *testing.T) {
	registry := provisioner.NewRegistry()
	want := registry.AllSlugs()

	for _, slug := range want {
		s := &config.Stack{
			Name:    "parity",
			Network: config.Network{Name: "parity-net"},
			Link:    []config.LinkEntry{{Client: slug}},
		}
		if err := config.Validate(s); err != nil {
			t.Errorf("registry slug %q rejected by validateLinks: %v", slug, err)
		}
	}

	// The reverse direction: every slug config accepts must exist in the
	// registry. Probe with the documented list rather than exporting the map.
	for _, slug := range config.SupportedLinkClientsForTest() {
		if _, ok := registry.FindBySlug(slug); !ok {
			t.Errorf("config accepts slug %q that the provisioner registry does not know", slug)
		}
	}
}
