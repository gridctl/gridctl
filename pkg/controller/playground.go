package controller

import (
	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/llm/anthropic"
	"github.com/gridctl/gridctl/pkg/agent/llm/gateway"
	"github.com/gridctl/gridctl/pkg/agent/llm/google"
	"github.com/gridctl/gridctl/pkg/agent/llm/openai"
	"github.com/gridctl/gridctl/pkg/vault"
)

// buildPlaygroundProvider assembles the LLM provider for the
// playground surface from whichever vault keys resolve. The returned
// provider is a prefix-routing gateway that dispatches by model name —
// "claude-*" → Anthropic, "gpt-*" / "o1-*" / "o3-*" → OpenAI,
// "gemini-*" → Google. Returns nil when no provider key resolves; the
// API server treats nil as "playground disabled" and surfaces a clear
// error to the user.
//
// Provider API keys resolve through the vault exclusively; when no
// vault is configured the function returns nil. Phase C extends this
// to honor ${vault:KEY} references on a per-skill basis.
func buildPlaygroundProvider(store *vault.Store) agent.ChatModel {
	if store == nil {
		return nil
	}

	var opts []gateway.Option

	if key, ok := vaultLookup(store, "ANTHROPIC_API_KEY"); ok {
		if p, err := anthropic.New(key); err == nil {
			opts = append(opts, gateway.WithRoute("claude-", p))
		}
	}
	if key, ok := vaultLookup(store, "OPENAI_API_KEY"); ok {
		if p, err := openai.New(key); err == nil {
			opts = append(opts, gateway.WithRoute("gpt-", p))
			opts = append(opts, gateway.WithRoute("o1-", p))
			opts = append(opts, gateway.WithRoute("o3-", p))
		}
	}
	if key, ok := googleVaultLookup(store); ok {
		if p, err := google.New(key); err == nil {
			opts = append(opts, gateway.WithRoute("gemini-", p))
		}
	}

	if len(opts) == 0 {
		return nil
	}
	provider, err := gateway.New(opts...)
	if err != nil {
		return nil
	}
	return provider
}

// vaultLookup returns (value, true) when the named key resolves to a
// non-empty secret, otherwise ("", false). Wraps store.Get so callers
// branch on the presence of a value rather than the empty string.
func vaultLookup(store *vault.Store, key string) (string, bool) {
	v, ok := store.Get(key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// googleVaultLookup tries the two conventional Gemini-key names in
// priority order: GEMINI_API_KEY first (the user-facing convention),
// then GOOGLE_API_KEY (legacy / Google Cloud console default).
func googleVaultLookup(store *vault.Store) (string, bool) {
	if v, ok := vaultLookup(store, "GEMINI_API_KEY"); ok {
		return v, true
	}
	return vaultLookup(store, "GOOGLE_API_KEY")
}
