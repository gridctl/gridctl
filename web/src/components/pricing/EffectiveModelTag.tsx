import { cn } from '../../lib/cn';
import type { EffectiveModel } from '../../types';
import { mixedTooltip, unpricedTooltip, effectiveAriaLabel, sharePct } from './effectiveModel';

// EffectiveModelTag renders the read-only `mixed` and `none` provenance
// states shared by the client and server cells. Declared models keep their
// existing pill in the host cell; this component only covers the two
// non-declared states so the common case is visually unchanged.
//
//   mixed → "<dominant> · NN%" muted pill, hover shows the histogram + the
//           "declared, not observed" honesty note; click opens the manager.
//   none  → muted "unpriced", hover explains cost is $0.
//
// Provenance is conveyed by visible text (not color alone) and an aria-label
// carrying the full sentence, per the accessibility contract.
export function EffectiveModelTag({
  effective,
  onClick,
}: {
  effective: EffectiveModel;
  onClick?: () => void;
}) {
  if (effective.provenance === 'mixed') {
    const dominant = effective.model ?? '—';
    const pct = sharePct(effective.share ?? 0);
    return (
      <button
        type="button"
        onClick={onClick}
        disabled={!onClick}
        title={mixedTooltip(effective)}
        aria-label={effectiveAriaLabel(effective)}
        className={cn(
          'inline-flex items-center gap-1 rounded-full border border-dashed border-border/50 px-2 py-0.5',
          'bg-surface-highlight/40 transition-colors',
          onClick && 'hover:border-primary/40 cursor-pointer',
        )}
      >
        <span className="text-[10px] font-mono text-text-secondary">{dominant}</span>
        <span className="text-[9px] text-text-muted/70">· {pct} · mixed</span>
      </button>
    );
  }

  // none
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!onClick}
      title={unpricedTooltip()}
      aria-label={effectiveAriaLabel(effective)}
      className={cn(
        'text-[10px] text-text-muted/70 transition-colors',
        onClick && 'hover:text-secondary cursor-pointer',
      )}
    >
      unpriced
    </button>
  );
}

export default EffectiveModelTag;
