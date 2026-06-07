package api

import (
	"sort"

	"github.com/gridctl/gridctl/pkg/metrics"
)

// Model provenance values carried on every EffectiveModel. The set is an
// open string enum so future signals can extend it without reshaping the
// API. `reported` is reserved for a future protocol-level model signal (an
// upstream client declaring its model on the wire) and is unused today.
const (
	provenanceDeclared = "declared" // one model priced all of this entity's recorded cost
	provenanceMixed    = "mixed"    // two or more models priced this entity's traffic
	provenanceNone     = "none"     // traffic observed but no cost recorded (no attribution)
	// provenanceReported is intentionally unused in v1. Declared here so the
	// contract is documented in one place for when a wire signal lands.
	provenanceReported = "reported" //nolint:unused // reserved for a future protocol-level model signal
)

// ModelShare is one model's slice of an entity's recorded cost: the model
// ID, its USD cost, and its share (0–1) of the entity's total recorded cost.
type ModelShare struct {
	Model   string  `json:"model"`
	CostUSD float64 `json:"cost_usd"`
	Share   float64 `json:"share"`
}

// EffectiveModel is the read-time interpretation of which model(s) priced an
// entity's (client or server) traffic, with provenance. It describes which
// declaration gridctl applied when pricing the traffic — NOT which model the
// upstream client actually used; the gateway cannot observe that. Model and
// Share describe the dominant entry; Models lists every model that priced the
// entity, descending by cost.
type EffectiveModel struct {
	Model      string       `json:"model,omitempty"`
	Provenance string       `json:"provenance"`
	Share      float64      `json:"share,omitempty"`
	Models     []ModelShare `json:"models,omitempty"`
}

// deriveEffectiveModels computes the effective model + provenance for every
// entity that has either recorded cost (a model histogram) or observed
// traffic (tokens). It is a pure function of the two snapshots so it is
// trivially testable.
//
//   - One histogram model        → declared (that model, share 1.0).
//   - Two or more histogram models → mixed (dominant by cost; full breakdown).
//   - Tokens but no histogram     → none (traffic priced as $0, no attribution).
//   - Neither                     → omitted.
//
// tokenTotals supplies the `none` case (it needs token volume, which the
// cost histogram does not carry). Both maps may be nil.
func deriveEffectiveModels(tokenTotals map[string]metrics.TokenCounts, histograms map[string]map[string]metrics.ModelCost) map[string]EffectiveModel {
	if len(tokenTotals) == 0 && len(histograms) == 0 {
		return nil
	}
	out := make(map[string]EffectiveModel)

	for entity, models := range histograms {
		if em, ok := effectiveFromHistogram(models); ok {
			out[entity] = em
		}
	}

	// Entities with traffic but no histogram are `none` (priced as $0).
	for entity, tokens := range tokenTotals {
		if _, done := out[entity]; done {
			continue
		}
		if tokens.TotalTokens > 0 {
			out[entity] = EffectiveModel{Provenance: provenanceNone}
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

// effectiveFromHistogram reduces one entity's model histogram to an
// EffectiveModel. Returns ok=false when the histogram is empty (the caller
// then considers the `none` case from token data).
func effectiveFromHistogram(models map[string]metrics.ModelCost) (EffectiveModel, bool) {
	if len(models) == 0 {
		return EffectiveModel{}, false
	}

	shares := make([]ModelShare, 0, len(models))
	var total float64
	for model, mc := range models {
		shares = append(shares, ModelShare{Model: model, CostUSD: mc.CostUSD})
		total += mc.CostUSD
	}
	// Descending by cost, then model ID ascending for a deterministic order
	// (and stable dominant selection on exact-cost ties).
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].CostUSD != shares[j].CostUSD {
			return shares[i].CostUSD > shares[j].CostUSD
		}
		return shares[i].Model < shares[j].Model
	})
	if total > 0 {
		for i := range shares {
			shares[i].Share = shares[i].CostUSD / total
		}
	}

	dominant := shares[0]
	provenance := provenanceDeclared
	if len(shares) > 1 {
		provenance = provenanceMixed
	}
	return EffectiveModel{
		Model:      dominant.Model,
		Provenance: provenance,
		Share:      dominant.Share,
		Models:     shares,
	}, true
}
