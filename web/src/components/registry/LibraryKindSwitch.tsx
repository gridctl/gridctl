import { cn } from '../../lib/cn';
import type { RegistryKind } from '../../lib/registryKind';

const KIND_OPTIONS: { key: RegistryKind; label: string }[] = [
  { key: 'skill', label: 'Skills' },
  { key: 'agent', label: 'Agents' },
];

/**
 * The Library's kind segment, standing where the static "library" eyebrow
 * label used to. Prominence-by-position is deliberate: the header already
 * carries five actions, and this control is the sole discovery mechanism
 * for the Agents catalog, which starts empty on nearly every install.
 * Styled at the eyebrow's scale with the workspace's aria-pressed button
 * treatment (GroupByControl / SortControl).
 */
export function LibraryKindSwitch({
  kind,
  onChange,
}: {
  kind: RegistryKind;
  onChange: (kind: RegistryKind) => void;
}) {
  return (
    <div className="flex items-center gap-1" role="group" aria-label="Library catalog kind">
      {KIND_OPTIONS.map((opt) => (
        <button
          key={opt.key}
          onClick={() => onChange(opt.key)}
          aria-pressed={kind === opt.key}
          className={cn(
            'px-2 py-1 rounded-md text-[10px] uppercase tracking-[0.25em] font-medium transition-colors border',
            kind === opt.key
              ? 'bg-primary/10 text-primary border-primary/25'
              : 'text-text-muted/60 hover:text-text-secondary hover:bg-surface-highlight border-transparent',
          )}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}
