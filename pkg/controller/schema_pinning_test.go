package controller

import (
	"errors"
	"os"
	"path/filepath"
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
		wantScan    bool
		wantIgnore  int
	}{
		{"nil stack defaults to on/warn/scan", nil, true, "warn", true, 0},
		{"no gateway block", &config.Stack{}, true, "warn", true, 0},
		{"no security block", &config.Stack{Gateway: &config.GatewayConfig{}}, true, "warn", true, 0},
		{"security but no schema_pinning", pinningStack(nil), true, "warn", true, 0},
		{"schema_pinning present, enabled omitted stays on", pinningStack(&config.SchemaPinningConfig{}), true, "warn", true, 0},
		{"explicit enabled false disables", pinningStack(&config.SchemaPinningConfig{Enabled: boolPtr(false)}), false, "warn", true, 0},
		{"action block with enabled omitted stays on", pinningStack(&config.SchemaPinningConfig{Action: "block"}), true, "block", true, 0},
		{"enabled true and action block", pinningStack(&config.SchemaPinningConfig{Enabled: boolPtr(true), Action: "block"}), true, "block", true, 0},
		{"unknown action falls back to warn", pinningStack(&config.SchemaPinningConfig{Action: "shout"}), true, "warn", true, 0},
		{"scan omitted stays on", pinningStack(&config.SchemaPinningConfig{Enabled: boolPtr(true)}), true, "warn", true, 0},
		{"explicit scan false disables scanning only", pinningStack(&config.SchemaPinningConfig{Scan: boolPtr(false)}), true, "warn", false, 0},
		{"scan_ignore passes through", pinningStack(&config.SchemaPinningConfig{ScanIgnore: []string{"P004", "P003"}}), true, "warn", true, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, action, scan, ignore := resolveSchemaPinning(tt.stack)
			if enabled != tt.wantEnabled || action != tt.wantAction || scan != tt.wantScan || len(ignore) != tt.wantIgnore {
				t.Errorf("resolveSchemaPinning() = (%v, %q, %v, %d codes), want (%v, %q, %v, %d codes)",
					enabled, action, scan, len(ignore), tt.wantEnabled, tt.wantAction, tt.wantScan, tt.wantIgnore)
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

// --- loadPinStore policy ---

func TestLoadPinStore_MissingFileStartsEmpty(t *testing.T) {
	ps, err := loadPinStore(pins.NewWithPath(t.TempDir(), "fresh"), "fresh")
	if err != nil {
		t.Fatalf("missing file must be the normal first run, got: %v", err)
	}
	if ps == nil {
		t.Fatal("expected a usable store")
	}
}

func TestLoadPinStore_CorruptFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("{truncated"), 0600); err != nil {
		t.Fatal(err)
	}

	ps, err := loadPinStore(pins.NewWithPath(dir, "bad"), "bad")
	if err != nil {
		t.Fatalf("corrupt file must self-heal so the daemon starts, got: %v", err)
	}
	if got := len(ps.GetAll()); got != 0 {
		t.Errorf("expected empty store after corrupt load, got %d servers", got)
	}
}

func TestLoadPinStore_NewerVersionAborts(t *testing.T) {
	dir := t.TempDir()
	content := `{"version":"99","stack":"future","created_at":"2026-01-01T00:00:00Z","servers":{}}`
	if err := os.WriteFile(filepath.Join(dir, "future.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	ps, err := loadPinStore(pins.NewWithPath(dir, "future"), "future")
	if !errors.Is(err, pins.ErrNewerVersion) {
		t.Fatalf("a newer-version pin file must abort the build, got ps=%v err=%v", ps, err)
	}
	if ps != nil {
		t.Error("no store may be returned when the load aborts; an empty store would overwrite the newer file")
	}
}
