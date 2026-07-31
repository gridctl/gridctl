package controller

import (
	"log/slog"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
)

// TestRefreshExperimentalFlags exercises the resolve-and-store path the
// /api/status features closure reads through, including the hot-reload swap.
func TestRefreshExperimentalFlags(t *testing.T) {
	logger := slog.Default()

	t.Run("omitted block resolves to no features (back-compat)", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(&config.Stack{}, logger)
		state := b.experimentalFlags.Load()
		if state == nil {
			t.Fatal("refresh must always store a state")
		}
		if len(state.features) != 0 {
			t.Fatalf("features = %+v, want none", state.features)
		}
	})

	t.Run("enabled registered flag surfaces with metadata", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(&config.Stack{
			Experimental: map[string]bool{"transport_dual_stack": true},
		}, logger)
		features := b.experimentalFlags.Load().features
		if len(features) != 1 {
			t.Fatalf("features = %+v, want exactly one", features)
		}
		f := features[0]
		if f.Name != "transport_dual_stack" || f.Stage != "experimental" || f.Description == "" {
			t.Fatalf("feature = %+v", f)
		}
	})

	t.Run("disabled and unknown flags do not surface", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(&config.Stack{
			Experimental: map[string]bool{
				"transport_dual_stack": false,
				"not_a_real_flag":      true,
			},
		}, logger)
		if features := b.experimentalFlags.Load().features; len(features) != 0 {
			t.Fatalf("features = %+v, want none", features)
		}
	})

	t.Run("hot reload swaps the stored set", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(&config.Stack{
			Experimental: map[string]bool{"transport_dual_stack": true},
		}, logger)
		if len(b.experimentalFlags.Load().features) != 1 {
			t.Fatal("setup: expected one feature before reload")
		}
		b.refreshExperimentalFlags(&config.Stack{}, logger)
		if features := b.experimentalFlags.Load().features; len(features) != 0 {
			t.Fatalf("features after reload = %+v, want none", features)
		}
	})

	t.Run("env override enables without yaml", func(t *testing.T) {
		t.Setenv("GRIDCTL_EXPERIMENTAL_TRANSPORT_DUAL_STACK", "true")
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(&config.Stack{}, logger)
		features := b.experimentalFlags.Load().features
		if len(features) != 1 || features[0].Name != "transport_dual_stack" {
			t.Fatalf("features = %+v, want transport_dual_stack via env", features)
		}
	})
}
