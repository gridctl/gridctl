import { cn } from '../../lib/cn';
import { modelChipInfo } from '../../lib/modelPreference';
import type { ModelPreference } from '../../types';

/**
 * Read-only model preference chip, PackChip's sibling in the provenance
 * chip family. Two visual states: an author declaration renders neutral;
 * a policy-resolved value (a stack default or override deciding what
 * projection applies) renders in the primary accent with a "policy"
 * suffix so provenance is never ambiguous. A preference is a durable
 * default the client may still override — the copy never says more.
 */
export function ModelChip({
  modelPreference,
  className,
}: {
  modelPreference: ModelPreference | undefined | null;
  className?: string;
}) {
  const info = modelChipInfo(modelPreference);
  if (!info) return null;
  const title = info.viaPolicy
    ? `Stack model preference policy ${info.resolution === 'override' ? 'override' : 'default'} applies ${info.value}${
        info.declared ? ` (author declared ${info.declared})` : ' (author declared nothing)'
      }`
    : `Author-declared model preference: ${info.value}`;
  return (
    <span
      title={title}
      data-testid="model-chip"
      className={cn(
        'text-[9px] font-medium tracking-wider px-1.5 py-0.5 rounded-full border flex-shrink-0 font-mono lowercase',
        info.viaPolicy
          ? 'border-primary/30 bg-primary/10 text-primary'
          : 'border-border/50 bg-surface-highlight/60 text-text-muted',
        className,
      )}
    >
      {info.value}
      {info.viaPolicy && <span className="opacity-70"> · policy</span>}
    </span>
  );
}

/** Honor status vocabulary from the backend matrix, verbatim. */
const HONOR_LABELS: Record<string, string> = {
  honored: 'honored',
  ignored: 'ignored',
  unknown: 'unknown',
  'dropped-on-render': 'dropped on render',
};

const HONOR_DOT: Record<string, string> = {
  honored: 'bg-status-running',
  ignored: 'bg-status-idle',
  unknown: 'bg-status-pending',
  'dropped-on-render': 'bg-status-idle',
};

/** Display names for projection target slugs across both kinds. */
const TARGET_NAMES: Record<string, string> = {
  'claude-code': 'Claude Code',
  agents: 'Agents interop dir',
  antigravity: 'Antigravity',
  opencode: 'OpenCode',
  copilot: 'GitHub Copilot',
  gemini: 'Gemini CLI',
};

/**
 * Per-target honor rows: what each projection target does with a model
 * preference, straight from the wire matrix so silence never reads as
 * support. Renders nothing without a matrix.
 */
export function ModelHonorList({ honor }: { honor: Record<string, string> | undefined | null }) {
  const entries = Object.entries(honor ?? {}).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0) return null;
  return (
    <ul className="flex flex-col gap-1" data-testid="model-honor-list">
      {entries.map(([slug, status]) => (
        <li key={slug} className="flex items-center gap-2 text-[11px]">
          <span
            aria-hidden="true"
            className={cn('w-1.5 h-1.5 rounded-full flex-shrink-0', HONOR_DOT[status] ?? 'bg-status-pending')}
          />
          <span className="text-text-secondary flex-1 min-w-0 truncate">{TARGET_NAMES[slug] ?? slug}</span>
          <span
            className={cn(
              'font-mono text-[10px]',
              status === 'honored' ? 'text-text-secondary' : 'text-text-muted',
            )}
          >
            {HONOR_LABELS[status] ?? status}
          </span>
        </li>
      ))}
    </ul>
  );
}
