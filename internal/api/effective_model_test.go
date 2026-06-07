package api

import (
	"testing"

	"github.com/gridctl/gridctl/pkg/metrics"
)

func approxEq(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

func TestDeriveEffectiveModels_Declared(t *testing.T) {
	histograms := map[string]map[string]metrics.ModelCost{
		"claude-code": {"claude-opus-4-7": {CostUSD: 1.50, InputTokens: 100, OutputTokens: 50}},
	}
	tokens := map[string]metrics.TokenCounts{
		"claude-code": {InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}

	got := deriveEffectiveModels(tokens, histograms)
	em, ok := got["claude-code"]
	if !ok {
		t.Fatalf("expected entry for claude-code; got %+v", got)
	}
	if em.Provenance != provenanceDeclared {
		t.Errorf("provenance = %q, want declared", em.Provenance)
	}
	if em.Model != "claude-opus-4-7" {
		t.Errorf("model = %q, want claude-opus-4-7", em.Model)
	}
	if !approxEq(em.Share, 1.0) {
		t.Errorf("share = %v, want 1.0", em.Share)
	}
	if len(em.Models) != 1 {
		t.Errorf("models len = %d, want 1", len(em.Models))
	}
}

func TestDeriveEffectiveModels_Mixed(t *testing.T) {
	histograms := map[string]map[string]metrics.ModelCost{
		"cursor": {
			"claude-opus-4-7":  {CostUSD: 0.90},
			"claude-haiku-4-5": {CostUSD: 0.10},
		},
	}
	tokens := map[string]metrics.TokenCounts{
		"cursor": {TotalTokens: 400},
	}

	got := deriveEffectiveModels(tokens, histograms)
	em := got["cursor"]
	if em.Provenance != provenanceMixed {
		t.Fatalf("provenance = %q, want mixed", em.Provenance)
	}
	if em.Model != "claude-opus-4-7" {
		t.Errorf("dominant model = %q, want claude-opus-4-7", em.Model)
	}
	if !approxEq(em.Share, 0.9) {
		t.Errorf("dominant share = %v, want 0.9", em.Share)
	}
	if len(em.Models) != 2 {
		t.Fatalf("models len = %d, want 2", len(em.Models))
	}
	// Descending by cost: opus first.
	if em.Models[0].Model != "claude-opus-4-7" || em.Models[1].Model != "claude-haiku-4-5" {
		t.Errorf("models not sorted by cost desc: %+v", em.Models)
	}
	// Shares sum to 1.0.
	if !approxEq(em.Models[0].Share+em.Models[1].Share, 1.0) {
		t.Errorf("shares do not sum to 1.0: %+v", em.Models)
	}
}

func TestDeriveEffectiveModels_None(t *testing.T) {
	// Tokens observed but no cost histogram → none.
	tokens := map[string]metrics.TokenCounts{
		"gemini-cli": {InputTokens: 200, OutputTokens: 100, TotalTokens: 300},
	}
	got := deriveEffectiveModels(tokens, nil)
	em, ok := got["gemini-cli"]
	if !ok {
		t.Fatalf("expected none entry; got %+v", got)
	}
	if em.Provenance != provenanceNone {
		t.Errorf("provenance = %q, want none", em.Provenance)
	}
	if em.Model != "" || len(em.Models) != 0 {
		t.Errorf("none entry should carry no model; got %+v", em)
	}
}

func TestDeriveEffectiveModels_NoTrafficOmitted(t *testing.T) {
	tokens := map[string]metrics.TokenCounts{
		"idle": {TotalTokens: 0},
	}
	got := deriveEffectiveModels(tokens, nil)
	if _, ok := got["idle"]; ok {
		t.Errorf("entity with zero traffic should be omitted; got %+v", got)
	}
}

func TestDeriveEffectiveModels_DeclaredWinsOverNone(t *testing.T) {
	// An entity present in both tokens and histogram derives from the
	// histogram (declared), not the token-only none path.
	histograms := map[string]map[string]metrics.ModelCost{
		"claude-code": {"claude-opus-4-7": {CostUSD: 1.0}},
	}
	tokens := map[string]metrics.TokenCounts{
		"claude-code": {TotalTokens: 150},
	}
	got := deriveEffectiveModels(tokens, histograms)
	if got["claude-code"].Provenance != provenanceDeclared {
		t.Errorf("provenance = %q, want declared", got["claude-code"].Provenance)
	}
}

func TestDeriveEffectiveModels_EmptyInputsNil(t *testing.T) {
	if got := deriveEffectiveModels(nil, nil); got != nil {
		t.Errorf("empty inputs should derive nil; got %+v", got)
	}
}
