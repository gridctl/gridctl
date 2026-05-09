package controller

import (
	"context"
	"testing"

	"github.com/gridctl/gridctl/pkg/agent/llm/gateway"
	"github.com/gridctl/gridctl/pkg/vault"
)

func TestBuildPlaygroundProvider_NilStore(t *testing.T) {
	t.Parallel()
	if p := buildPlaygroundProvider(nil); p != nil {
		t.Errorf("buildPlaygroundProvider(nil) = %v, want nil", p)
	}
}

func TestBuildPlaygroundProvider_EmptyStore(t *testing.T) {
	t.Parallel()
	store := vault.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p := buildPlaygroundProvider(store); p != nil {
		t.Errorf("expected nil with no keys configured, got %T", p)
	}
}

func TestBuildPlaygroundProvider_AnthropicKey(t *testing.T) {
	t.Parallel()
	store := vault.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := store.Set("ANTHROPIC_API_KEY", "sk-ant-test"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	provider := buildPlaygroundProvider(store)
	if provider == nil {
		t.Fatal("expected provider, got nil")
	}
	gw, ok := provider.(*gateway.Provider)
	if !ok {
		t.Fatalf("provider type = %T, want *gateway.Provider", provider)
	}
	routes := gw.Routes()
	if len(routes) != 1 {
		t.Fatalf("len(routes) = %d, want 1", len(routes))
	}
	if routes[0].Prefix != "claude-" {
		t.Errorf("route prefix = %q, want claude-", routes[0].Prefix)
	}
}

func TestBuildPlaygroundProvider_AllProviders(t *testing.T) {
	t.Parallel()
	store := vault.NewStore(t.TempDir())
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	for k, v := range map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-test",
		"OPENAI_API_KEY":    "sk-openai-test",
		"GEMINI_API_KEY":    "test",
	} {
		if err := store.Set(k, v); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}
	provider := buildPlaygroundProvider(store)
	if provider == nil {
		t.Fatal("expected provider, got nil")
	}
	gw := provider.(*gateway.Provider)
	if len(gw.Routes()) < 4 {
		// claude- + gpt- + o1- + o3- + gemini-
		t.Errorf("len(routes) = %d, want >= 4", len(gw.Routes()))
	}

	// Verify it actually satisfies agent.ChatModel — type-only test
	// is fine; we are not making a real HTTP call here.
	var _ = provider // explicit interface check is via the function signature
	_ = context.Background()
}

func TestVaultLookup_EmptyValueIsAbsent(t *testing.T) {
	t.Parallel()
	store := vault.NewStore(t.TempDir())
	_ = store.Load()
	if err := store.Set("EMPTY", ""); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok := vaultLookup(store, "EMPTY"); ok {
		t.Errorf("vaultLookup returned ok for empty value: %q", v)
	}
}

func TestGoogleVaultLookup_PrefersGemini(t *testing.T) {
	t.Parallel()
	store := vault.NewStore(t.TempDir())
	_ = store.Load()
	_ = store.Set("GEMINI_API_KEY", "gemini")
	_ = store.Set("GOOGLE_API_KEY", "google")
	v, ok := googleVaultLookup(store)
	if !ok || v != "gemini" {
		t.Errorf("got %q, ok=%v; want gemini, true", v, ok)
	}
}

func TestGoogleVaultLookup_FallsBackToGoogle(t *testing.T) {
	t.Parallel()
	store := vault.NewStore(t.TempDir())
	_ = store.Load()
	_ = store.Set("GOOGLE_API_KEY", "google")
	v, ok := googleVaultLookup(store)
	if !ok || v != "google" {
		t.Errorf("got %q, ok=%v; want google, true", v, ok)
	}
}
