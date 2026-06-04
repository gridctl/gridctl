package pricing

import (
	_ "embed"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// rawModelPrices is the embedded LiteLLM pricing snapshot. Refresh via
// `make update-pricing` (see Makefile).
//
//go:embed data/model_prices.json
var rawModelPrices []byte

// litellmEntry mirrors the subset of LiteLLM's per-model record we care
// about. Fields beyond these are tolerated and ignored — keeping the struct
// narrow means we are not coupled to LiteLLM's full schema.
type litellmEntry struct {
	InputCostPerToken           float64 `json:"input_cost_per_token"`
	OutputCostPerToken          float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost     float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost float64 `json:"cache_creation_input_token_cost"`
	LiteLLMProvider             string  `json:"litellm_provider"`
	Mode                        string  `json:"mode"`
}

// LiteLLMSource is a Source backed by an embedded snapshot of LiteLLM's
// model_prices_and_context_window.json. The full file is parsed once at
// construction and held in memory; lookups are constant-time map reads.
type LiteLLMSource struct {
	rates  map[string]Rates // canonical model ID -> rates
	models []string         // sorted canonical model IDs (cached for Models)
	warn   warnOnce         // logs unknown models a single time per ID
}

// NewLiteLLMSource parses the embedded LiteLLM pricing data and returns a
// ready-to-use Source. Parsing failures fall back to an empty rate table
// (every Lookup returns false). Failures are logged at WARN; callers do not
// receive an error so the gateway can still start when pricing data is
// malformed.
func NewLiteLLMSource() *LiteLLMSource {
	rates := parseLiteLLMRates(rawModelPrices)
	models := make([]string, 0, len(rates))
	for id := range rates {
		models = append(models, id)
	}
	sort.Strings(models)
	return &LiteLLMSource{rates: rates, models: models}
}

// Name returns "litellm".
func (s *LiteLLMSource) Name() string { return "litellm" }

// Models returns the sorted canonical model IDs in the rate table, computed
// once at construction. Callers must not mutate the returned slice.
func (s *LiteLLMSource) Models() []string { return s.models }

// Lookup returns the per-token rates for the given model ID. Probes the
// rate table in order from most specific to most general:
//
//  1. The exact normalized form (provider stripped, lower-cased).
//  2. The same form with any trailing -YYYYMMDD date suffix removed.
//  3. A small alias table for IDs that diverge by more than the
//     prefix/date heuristics handle.
//
// The lookup path is allocation-free for already-canonical IDs (e.g.
// "claude-opus-4-7") so the cost path can run on the gateway hot path
// without per-call GC pressure.
func (s *LiteLLMSource) Lookup(model string) (Rates, bool) {
	canonical := canonicalKey(model)
	if canonical == "" {
		return Rates{}, false
	}
	if r, ok := s.rates[canonical]; ok {
		return r, true
	}
	if stripped, dropped := stripDateSuffix(canonical); dropped {
		if r, ok := s.rates[stripped]; ok {
			return r, true
		}
	}
	if alias, ok := modelAliases[canonical]; ok {
		if r, ok := s.rates[alias]; ok {
			return r, true
		}
	}
	s.warn.do(model, func() {
		slog.Default().Warn("pricing: unknown model, treating as zero cost",
			"source", "litellm",
			"model", model,
		)
	})
	return Rates{}, false
}

// parseLiteLLMRates decodes raw LiteLLM JSON into a normalized rate map.
// The "sample_spec" sentinel entry — which uses string-typed fields for
// documentation — is skipped via per-entry unmarshal (a top-level
// map[string]litellmEntry would fail on the sentinel's mixed types).
func parseLiteLLMRates(data []byte) map[string]Rates {
	if len(data) == 0 {
		return map[string]Rates{}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		slog.Default().Warn("pricing: failed to parse litellm data, lookups will be unavailable",
			"error", err)
		return map[string]Rates{}
	}
	out := make(map[string]Rates, len(raw))
	for key, payload := range raw {
		if key == "sample_spec" {
			continue
		}
		var entry litellmEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			// Mixed-type entries (e.g. those that mirror sample_spec) are skipped
			// silently. The empty result for that key already conveys "unknown".
			continue
		}
		if entry.InputCostPerToken == 0 && entry.OutputCostPerToken == 0 &&
			entry.CacheReadInputTokenCost == 0 && entry.CacheCreationInputTokenCost == 0 {
			continue
		}
		canonical := canonicalKey(key)
		out[canonical] = Rates{
			InputPerToken:      entry.InputCostPerToken,
			OutputPerToken:     entry.OutputCostPerToken,
			CacheReadPerToken:  entry.CacheReadInputTokenCost,
			CacheWritePerToken: entry.CacheCreationInputTokenCost,
		}
	}
	return out
}

// canonicalKey returns the lower-cased provider-stripped form of a LiteLLM
// key. Provider prefixes ("anthropic/", "openai/", etc.) are dropped so
// that "anthropic/claude-opus-4-7" and "claude-opus-4-7" share storage.
// strings.ToLower returns the input unchanged when it is already lower
// case, and strings.TrimSpace returns the input unchanged when there is
// no leading/trailing whitespace, so callers passing canonical IDs in
// steady state pay no allocation here.
func canonicalKey(s string) string {
	s = strings.TrimSpace(s)
	if hasUpper(s) {
		s = strings.ToLower(s)
	}
	if i := strings.Index(s, "/"); i != -1 {
		s = s[i+1:]
	}
	return s
}

// hasUpper reports whether s contains an ASCII uppercase letter. Used to
// avoid the strings.ToLower allocation for already-lowercase IDs.
func hasUpper(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			return true
		}
	}
	return false
}

// stripDateSuffix removes a trailing -YYYYMMDD suffix (8 digits preceded by
// a dash) and reports whether the suffix was present. Manual digit check
// instead of a regexp so the hot path stays allocation-free.
func stripDateSuffix(s string) (string, bool) {
	if len(s) < 9 || s[len(s)-9] != '-' {
		return s, false
	}
	for i := len(s) - 8; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return s, false
		}
	}
	return s[:len(s)-9], true
}

// candidateKeys returns the ordered list of keys candidate for a given
// model ID, used by tests to exercise the normalization rules without
// depending on the embedded rate table. Production code uses LiteLLMSource.Lookup
// directly, which probes the same keys without allocating a slice.
func candidateKeys(model string) []string {
	primary := canonicalKey(model)
	if primary == "" {
		return nil
	}
	keys := []string{primary}
	if stripped, dropped := stripDateSuffix(primary); dropped {
		keys = append(keys, stripped)
	}
	if alias, ok := modelAliases[primary]; ok {
		keys = append(keys, alias)
	}
	return keys
}

// modelAliases maps client-emitted IDs to LiteLLM canonical keys when the
// two diverge by more than the provider-prefix / date-suffix heuristics
// handle. Kept small on purpose — every entry is a maintenance cost.
var modelAliases = map[string]string{
	// Anthropic "-latest" pointers are not in the LiteLLM table; pin them
	// to the matching family's snapshot.
	"claude-opus-4-latest":   "claude-opus-4-7",
	"claude-sonnet-4-latest": "claude-sonnet-4-6",
	"claude-haiku-4-latest":  "claude-haiku-4-5",
	"claude-3-5-sonnet":      "claude-3-5-sonnet-20240620",
	"claude-3-5-haiku":       "claude-3-5-haiku-20241022",
	"claude-3-opus":          "claude-3-opus-20240229",
	"claude-3-sonnet":        "claude-3-sonnet-20240229",
	"claude-3-haiku":         "claude-3-haiku-20240307",
	// OpenAI "gpt-4-turbo" historically resolves to the preview snapshot.
	"gpt-4-turbo": "gpt-4-turbo-preview",
}

// warnOnce logs each unknown model ID a single time. The map grows with
// the number of distinct unknown models — bounded in practice by the set
// of models a deployment actually uses, so unbounded growth is not a
// realistic concern.
type warnOnce struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

func (w *warnOnce) do(key string, fn func()) {
	w.mu.Lock()
	if w.seen == nil {
		w.seen = make(map[string]struct{})
	}
	if _, ok := w.seen[key]; ok {
		w.mu.Unlock()
		return
	}
	w.seen[key] = struct{}{}
	w.mu.Unlock()
	fn()
}
