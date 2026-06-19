package controller

import (
	"testing"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

func boolPtr(b bool) *bool { return &b }

// pinningStack builds a stack whose gateway security block carries the given
// schema-pinning config. A nil sp leaves the security block present but empty.
func pinningStack(sp *config.SchemaPinningConfig) *config.Stack {
	return &config.Stack{
		Gateway: &config.GatewayConfig{
			Security: &config.GatewaySecurityConfig{SchemaPinning: sp},
		},
	}
}

func TestResolveSchemaPinning(t *testing.T) {
	tests := []struct {
		name        string
		stack       *config.Stack
		wantEnabled bool
		wantAction  string
	}{
		{"nil stack defaults to on/warn", nil, true, "warn"},
		{"no gateway block", &config.Stack{}, true, "warn"},
		{"no security block", &config.Stack{Gateway: &config.GatewayConfig{}}, true, "warn"},
		{"security but no schema_pinning", pinningStack(nil), true, "warn"},
		{"schema_pinning present, enabled omitted stays on", pinningStack(&config.SchemaPinningConfig{}), true, "warn"},
		{"explicit enabled false disables", pinningStack(&config.SchemaPinningConfig{Enabled: boolPtr(false)}), false, "warn"},
		{"action block with enabled omitted stays on", pinningStack(&config.SchemaPinningConfig{Action: "block"}), true, "block"},
		{"enabled true and action block", pinningStack(&config.SchemaPinningConfig{Enabled: boolPtr(true), Action: "block"}), true, "block"},
		{"unknown action falls back to warn", pinningStack(&config.SchemaPinningConfig{Action: "shout"}), true, "warn"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, action := resolveSchemaPinning(tt.stack)
			if enabled != tt.wantEnabled || action != tt.wantAction {
				t.Errorf("resolveSchemaPinning() = (%v, %q), want (%v, %q)",
					enabled, action, tt.wantEnabled, tt.wantAction)
			}
		})
	}
}

func TestInstallSchemaPinning(t *testing.T) {
	newPair := func() (*mcp.Gateway, *api.Server) {
		gw := mcp.NewGateway()
		return gw, api.NewServer(gw, nil)
	}
	store := pins.NewWithPath(t.TempDir(), "test-stack")

	t.Run("enabled installs verifier and API store", func(t *testing.T) {
		gw, srv := newPair()
		installSchemaPinning(gw, srv, &config.Stack{}, store)
		if gw.SchemaVerifier() == nil {
			t.Error("expected gateway schema verifier to be installed")
		}
		if srv.PinStore() == nil {
			t.Error("expected API server pin store to be set")
		}
	})

	t.Run("disabled installs neither half", func(t *testing.T) {
		gw, srv := newPair()
		installSchemaPinning(gw, srv, pinningStack(&config.SchemaPinningConfig{Enabled: boolPtr(false)}), store)
		if gw.SchemaVerifier() != nil {
			t.Error("expected no schema verifier when pinning disabled")
		}
		if srv.PinStore() != nil {
			t.Error("expected no pin store when pinning disabled")
		}
	})

	t.Run("nil store is a no-op", func(t *testing.T) {
		gw, srv := newPair()
		installSchemaPinning(gw, srv, &config.Stack{}, nil)
		if gw.SchemaVerifier() != nil || srv.PinStore() != nil {
			t.Error("expected no-op when store is nil")
		}
	})
}
