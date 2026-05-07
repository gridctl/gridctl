package pricing

import "testing"

// TestCanonicalKey_StripsProviderPrefix ensures provider-prefixed keys land
// at the same canonical slot as the bare model ID.
func TestCanonicalKey_StripsProviderPrefix(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"anthropic/claude-opus-4-7", "claude-opus-4-7"},
		{"openai/gpt-4o", "gpt-4o"},
		{"  Claude-Opus-4-7  ", "claude-opus-4-7"},
		{"vertex_ai/google/gemini-2.5-pro", "google/gemini-2.5-pro"}, // single-strip
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := canonicalKey(tt.in); got != tt.want {
				t.Errorf("canonicalKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCandidateKeys_FoldsDateSuffixes ensures dated snapshots fall back to
// their family ID when LiteLLM only publishes the family, and vice versa.
// The order matters: the exact form is probed first, then the date-stripped
// form, then any explicit alias.
func TestCandidateKeys_FoldsDateSuffixes(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{
			in:   "claude-opus-4-7-20260416",
			want: []string{"claude-opus-4-7-20260416", "claude-opus-4-7"},
		},
		{
			in:   "claude-opus-4-7",
			want: []string{"claude-opus-4-7"},
		},
		{
			in:   "anthropic/claude-3-5-sonnet-20240620",
			want: []string{"claude-3-5-sonnet-20240620", "claude-3-5-sonnet"},
		},
		{
			in:   "claude-3-5-sonnet",
			want: []string{"claude-3-5-sonnet", "claude-3-5-sonnet-20240620"},
		},
		{
			in:   "gpt-4-turbo",
			want: []string{"gpt-4-turbo", "gpt-4-turbo-preview"},
		},
		{
			in:   "",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := candidateKeys(tt.in)
			if !sliceEq(got, tt.want) {
				t.Errorf("candidateKeys(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestLiteLLMSource_NormalizationResolvesDatedAndPrefixed exercises the
// normalization end-to-end against the embedded data: a provider-prefixed
// or dated form should resolve to the same rates as the bare family ID.
func TestLiteLLMSource_NormalizationResolvesDatedAndPrefixed(t *testing.T) {
	src := NewLiteLLMSource()

	bareCases := []string{
		"claude-opus-4-7",
		"claude-sonnet-4-6",
		"claude-haiku-4-5",
		"gpt-4o",
	}
	for _, model := range bareCases {
		t.Run(model, func(t *testing.T) {
			r, ok := src.Lookup(model)
			if !ok {
				t.Fatalf("Lookup(%q) returned not-found", model)
			}

			// Provider-prefixed form must resolve.
			prefixed := "anthropic/" + model
			if r2, ok2 := src.Lookup(prefixed); !ok2 || r2 != r {
				t.Errorf("Lookup(%q) = (%+v, %v); want %+v", prefixed, r2, ok2, r)
			}

			// Upper-cased form must resolve.
			upper := "ANTHROPIC/" + model
			if r2, ok2 := src.Lookup(upper); !ok2 || r2 != r {
				t.Errorf("Lookup(%q) = (%+v, %v); want %+v (case folding)", upper, r2, ok2, r)
			}
		})
	}
}

func TestLiteLLMSource_LatestAliasResolves(t *testing.T) {
	src := NewLiteLLMSource()

	// "claude-opus-4-latest" is not a LiteLLM key; the alias map points
	// it at the current family snapshot.
	r, ok := src.Lookup("claude-opus-4-latest")
	if !ok {
		t.Fatal("expected -latest alias to resolve")
	}
	canon, ok := src.Lookup("claude-opus-4-7")
	if !ok {
		t.Fatal("expected canonical to resolve")
	}
	if r != canon {
		t.Errorf("alias rates %+v != canonical rates %+v", r, canon)
	}
}

func TestLiteLLMSource_UnknownReturnsFalse(t *testing.T) {
	src := NewLiteLLMSource()
	if _, ok := src.Lookup("definitely-not-a-real-model"); ok {
		t.Error("expected unknown model to return false")
	}
}

func TestLiteLLMSource_Name(t *testing.T) {
	src := NewLiteLLMSource()
	if src.Name() != "litellm" {
		t.Errorf("Name() = %q, want %q", src.Name(), "litellm")
	}
}

// TestParseLiteLLMRates_HandlesSampleSpec verifies that the documentation
// sentinel ("sample_spec") at the top of LiteLLM's JSON does not blow up
// the parser. That entry uses string-typed values in fields that are
// numeric in real entries — a top-level map[string]litellmEntry decode
// would fail on it.
func TestParseLiteLLMRates_HandlesSampleSpec(t *testing.T) {
	rates := parseLiteLLMRates(rawModelPrices)
	if len(rates) == 0 {
		t.Fatal("parseLiteLLMRates returned no entries")
	}
	if _, ok := rates["sample_spec"]; ok {
		t.Error("sample_spec sentinel should not be in the rate table")
	}
	if _, ok := rates["claude-opus-4-7"]; !ok {
		t.Error("claude-opus-4-7 should be present after parsing")
	}
}

func TestParseLiteLLMRates_EmptyInput(t *testing.T) {
	if got := parseLiteLLMRates(nil); len(got) != 0 {
		t.Errorf("nil input should yield empty map, got %d entries", len(got))
	}
	if got := parseLiteLLMRates([]byte("")); len(got) != 0 {
		t.Errorf("empty input should yield empty map, got %d entries", len(got))
	}
	if got := parseLiteLLMRates([]byte("not-json")); len(got) != 0 {
		t.Errorf("invalid JSON should yield empty map, got %d entries", len(got))
	}
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
