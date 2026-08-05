package controller

import (
	"log/slog"
	"testing"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/flags"
)

// testRegistry builds a registry with one synthetic experimental flag, so
// these tests keep covering the resolve-and-store mechanics regardless of
// what the built-in registry currently holds (its only entry has graduated).
func testRegistry(t *testing.T) *flags.Registry {
	t.Helper()
	reg, err := flags.NewRegistry(flags.Flag{
		Name:        "test_flag",
		Description: "synthetic flag for controller tests",
		Stage:       flags.StageExperimental,
		Since:       "0.1.0",
		GraduatesBy: "99.0.0",
	})
	if err != nil {
		t.Fatalf("building test registry: %v", err)
	}
	return reg
}

// TestRefreshExperimentalFlags exercises the resolve-and-store path the
// /api/status features closure reads through, including the hot-reload swap.
func TestRefreshExperimentalFlags(t *testing.T) {
	logger := slog.Default()

	t.Run("omitted block resolves to no features (back-compat)", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(testRegistry(t), &config.Stack{}, logger)
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
		b.refreshExperimentalFlags(testRegistry(t), &config.Stack{
			Experimental: map[string]bool{"test_flag": true},
		}, logger)
		features := b.experimentalFlags.Load().features
		if len(features) != 1 {
			t.Fatalf("features = %+v, want exactly one", features)
		}
		f := features[0]
		if f.Name != "test_flag" || f.Stage != "experimental" || f.Description == "" {
			t.Fatalf("feature = %+v", f)
		}
	})

	t.Run("disabled and unknown flags do not surface", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(testRegistry(t), &config.Stack{
			Experimental: map[string]bool{
				"test_flag":       false,
				"not_a_real_flag": true,
			},
		}, logger)
		if features := b.experimentalFlags.Load().features; len(features) != 0 {
			t.Fatalf("features = %+v, want none", features)
		}
	})

	t.Run("hot reload swaps the stored set", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(testRegistry(t), &config.Stack{
			Experimental: map[string]bool{"test_flag": true},
		}, logger)
		if len(b.experimentalFlags.Load().features) != 1 {
			t.Fatal("setup: expected one feature before reload")
		}
		b.refreshExperimentalFlags(testRegistry(t), &config.Stack{}, logger)
		if features := b.experimentalFlags.Load().features; len(features) != 0 {
			t.Fatalf("features after reload = %+v, want none", features)
		}
	})

	t.Run("env override enables without yaml", func(t *testing.T) {
		t.Setenv("GRIDCTL_EXPERIMENTAL_TEST_FLAG", "true")
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(testRegistry(t), &config.Stack{}, logger)
		features := b.experimentalFlags.Load().features
		if len(features) != 1 || features[0].Name != "test_flag" {
			t.Fatalf("features = %+v, want test_flag via env", features)
		}
	})

	t.Run("graduated builtin flag never surfaces", func(t *testing.T) {
		b := &GatewayBuilder{}
		b.refreshExperimentalFlags(flags.Default(), &config.Stack{
			Experimental: map[string]bool{"transport_dual_stack": true},
		}, logger)
		if features := b.experimentalFlags.Load().features; len(features) != 0 {
			t.Fatalf("features = %+v, want none for a graduated flag", features)
		}
	})
}
