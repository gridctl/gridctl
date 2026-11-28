import { cn } from '../../lib/cn';
import { SEGMENT_GROUP_CLASS, segmentClass } from '../ui/segment';
import type { RegistryKind } from '../../lib/registryKind';
import { usePendingProposalCount } from './proposals/usePendingProposalCount';

const KIND_OPTIONS: { key: RegistryKind; label: string; ariaLabel?: string; title?: string }[] = [
  { key: 'skill', label: 'Skills' },
  { key: 'agent', label: 'Agents' },
  { key: 'hook', label: 'Hooks' },
  { key: 'pack', label: 'Packs' },
  { key: 'context', label: 'Context', ariaLabel: 'Global Context', title: 'Global Context' },
  { key: 'proposal', label: 'Proposals' },
];

/**
 * The Library's kind segment, rendered in the workspace header's switcher
 * slot beside the title. Prominence-by-position is deliberate: the header
 * already carries several actions, and this control is the sole discovery
 * mechanism for the Agents catalog, which starts empty on nearly every
 * install. It shares the segmented Tabs chrome but stays an aria-pressed
 * button group rather than a tablist.
 *
 * The Proposals option carries a pending-count badge. The Proposals segment
 * passes its own live count; every other segment loads it once on mount.
 */
export function LibraryKindSwitch({
  kind,
  onChange,
  pendingProposals,
}: {
  kind: RegistryKind;
  onChange: (kind: RegistryKind) => void;
  pendingProposals?: number;
}) {
  const fetched = usePendingProposalCount(pendingProposals === undefined);
  const pending = pendingProposals ?? fetched ?? 0;
  return (
    <div className={cn(SEGMENT_GROUP_CLASS, 'flex-wrap')} role="group" aria-label="Library catalog kind">
      {KIND_OPTIONS.map((opt) => (
        <button
          key={opt.key}
          type="button"
          onClick={() => onChange(opt.key)}
          aria-pressed={kind === opt.key}
          aria-label={opt.ariaLabel}
          title={opt.title}
          className={cn(segmentClass(kind === opt.key), 'inline-flex items-center gap-1.5')}
        >
          {opt.label}
          {opt.key === 'proposal' && pending > 0 && (
            <span
              aria-label={`${pending} pending`}
              className="min-w-4 px-1 rounded-full bg-status-pending/15 text-status-pending text-3xs font-semibold"
            >
              {pending}
            </span>
          )}
        </button>
      ))}
    </div>
  );
}
