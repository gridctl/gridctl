import { useNavigate } from 'react-router';
import { cn } from '../../lib/cn';
import { useRegistryStore } from '../../stores/useRegistryStore';

/** The chip's visual treatment, shared with non-navigating variants
 *  (the Global Context dialog renders a static span inside its modal). */
export const PACK_CHIP_CLASS =
  'text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-secondary/30 bg-secondary/10 text-secondary flex-shrink-0';

/**
 * Reverse-ownership provenance chip: names the pack that owns a resource
 * and deep-links to the pack detail. Display only; pack management verbs
 * stay in the Packs segment.
 *
 * Two entry modes, one of which must be provided:
 *  - `pack`: the tag came over the wire (projection status rows).
 *  - `source`: join the packs list client-side by origin source (the
 *    #1079 pattern); renders nothing until that list has loaded or when
 *    no pack owns the source.
 *
 * Clicks stop propagation so the chip works inside clickable cards and
 * headers without triggering their selection.
 */
export function PackChip({
  pack,
  source,
  className,
}: {
  pack?: string;
  source?: string;
  className?: string;
}) {
  const navigate = useNavigate();
  const packs = useRegistryStore((s) => s.packs);
  const name =
    pack ?? (source ? (packs ?? []).find((p) => p.origin.source === source)?.name ?? null : null);
  if (!name) return null;
  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        navigate(`/library?kind=pack&selected=${encodeURIComponent(name)}`);
      }}
      title={`Imported through pack ${name}; open the pack detail`}
      className={cn(PACK_CHIP_CLASS, 'hover:bg-secondary/20 transition-colors', className)}
    >
      pack: {name}
    </button>
  );
}
