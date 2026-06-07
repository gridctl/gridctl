// Shared helpers for rendering effective-model provenance in the pricing
// cells, the sidebar inspector, and the pricing manager. Keeping the
// formatting in one place guarantees the metrics table and its detached
// twin read identically, and that every surface uses the same honesty copy.
import type { EffectiveModel, ModelShare } from '../../types';
import { MIXED_PROVENANCE_NOTE, UNPRICED_NOTE } from './constants';

// Percent label for a 0–1 share, e.g. 0.823 -> "82%".
export function sharePct(share: number): string {
  return `${Math.round(share * 100)}%`;
}

// Top-N models by cost, grouping the remainder into a single "other" row so a
// dense cell never lists a long tail. Shares are preserved from the backend
// (already descending); the grouped "other" sums the rest.
export function topShares(models: ModelShare[], n = 3): ModelShare[] {
  if (models.length <= n) return models;
  const head = models.slice(0, n);
  const tail = models.slice(n);
  const otherCost = tail.reduce((s, m) => s + m.cost_usd, 0);
  const otherShare = tail.reduce((s, m) => s + m.share, 0);
  return [...head, { model: 'other', cost_usd: otherCost, share: otherShare }];
}

// Native-title tooltip text for a mixed-provenance cell: the per-model
// breakdown followed by the honesty note. Plain text (no markup) so it works
// in a `title` attribute everywhere the pills render.
export function mixedTooltip(em: EffectiveModel): string {
  const lines = topShares(em.models ?? [])
    .map((m) => `${m.model} ${sharePct(m.share)}`)
    .join('  ·  ');
  return `Priced as: ${lines}\n${MIXED_PROVENANCE_NOTE}`;
}

// Tooltip text for a none-provenance cell.
export function unpricedTooltip(): string {
  return UNPRICED_NOTE;
}

// Accessible label for a non-default provenance state, carrying the full
// sentence so it reaches screen readers without relying on color or the
// truncated visible label.
export function effectiveAriaLabel(em: EffectiveModel): string {
  if (em.provenance === 'mixed') {
    const dominant = em.model ?? 'unknown';
    return `Mixed pricing: priced as ${dominant} at ${sharePct(em.share ?? 0)} of cost, plus other declared models. Click to edit pricing models.`;
  }
  if (em.provenance === 'none') {
    return 'Unpriced: traffic observed but no pricing model applied. Click to edit pricing models.';
  }
  return '';
}
