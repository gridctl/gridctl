import { CheckCircle2, GitBranch, Lock, LockOpen, ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { ServerPins, SkillPin } from '../../lib/api';
import { countServerAlertFindings, countSkillAlertFindings } from '../../stores/usePinsStore';
import { formatRelativeTime } from '../../lib/time';
import { pinStatusMeta } from './pinStatus';

// ---------------------------------------------------------------------------
// Left rail: kind toggle + attention toggle + server or skill rows
// ---------------------------------------------------------------------------

export type PinsKind = 'server' | 'skill';

interface PinsRailProps {
  compact: boolean;
  kind: PinsKind;
  onKindChange: (kind: PinsKind) => void;
  /** Server rows (already attention-filtered); rendered when kind=server. */
  entries: Array<[string, ServerPins]>;
  /** Skill rows (already attention-filtered); rendered when kind=skill. */
  skillEntries: Array<[string, SkillPin]>;
  /** Total pinned items of the active kind before filtering. */
  totalCount: number;
  activeName: string;
  onSelect: (name: string) => void;
  attentionOnly: boolean;
  onToggleAttention: () => void;
  /** Marks the row kept visible only because it is the active selection. */
  isOutsideFilter: (name: string) => boolean;
}

export function PinsRail({
  compact,
  kind,
  onKindChange,
  entries,
  skillEntries,
  totalCount,
  activeName,
  onSelect,
  attentionOnly,
  onToggleAttention,
  isOutsideFilter,
}: PinsRailProps) {
  const visibleCount = kind === 'server' ? entries.length : skillEntries.length;
  const noun = kind === 'server' ? 'server' : 'skill';
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div
        className={cn(
          'flex-shrink-0 px-3 border-b border-border-subtle/60 flex items-center justify-between gap-2',
          compact ? 'py-2' : 'py-3',
        )}
      >
        <div className="flex items-center gap-1" role="group" aria-label="Pin kind">
          {(['server', 'skill'] as const).map((k) => (
            <button
              key={k}
              onClick={() => onKindChange(k)}
              aria-pressed={kind === k}
              className={cn(
                'h-6 px-2 text-[10px] font-medium rounded border transition-colors',
                kind === k
                  ? 'bg-primary/10 text-primary border-primary/25'
                  : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60',
              )}
            >
              {k === 'server' ? 'Servers' : 'Skills'}
            </button>
          ))}
        </div>
        <button
          onClick={onToggleAttention}
          aria-pressed={attentionOnly}
          title={
            attentionOnly
              ? `Show all pinned ${noun}s`
              : `Show only ${noun}s with drift or warn-or-critical findings`
          }
          className={cn(
            'h-6 px-2 text-[10px] font-medium rounded border transition-colors flex items-center gap-1 flex-shrink-0',
            attentionOnly
              ? 'bg-status-pending/15 text-status-pending border-status-pending/30'
              : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60',
          )}
        >
          <ShieldAlert size={9} />
          Attention
        </button>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        {kind === 'server' &&
          entries.map(([name, sp]) => (
            <RailRow
              key={name}
              name={name}
              status={sp.status}
              findingCount={countServerAlertFindings(sp)}
              secondary={`${sp.tool_count} ${sp.tool_count === 1 ? 'tool' : 'tools'}${
                sp.last_verified_at
                  ? ` · verified ${formatRelativeTime(new Date(sp.last_verified_at))}`
                  : ''
              }`}
              active={name === activeName}
              outside={isOutsideFilter(name)}
              onSelect={onSelect}
            />
          ))}

        {kind === 'skill' &&
          skillRailItems(skillEntries).map((item) =>
            item.type === 'group' ? (
              // Multi-skill drift from one git source reviews as one unit:
              // a source header above its member rows.
              <div
                key={`group:${item.repo}`}
                className="flex items-center gap-1.5 px-3 pt-2 pb-0.5 text-[10px] text-text-muted/70"
                title={`${item.count} drifted skills from this source`}
              >
                <GitBranch size={9} />
                <span className="truncate">{item.repo}</span>
                <span className="flex-shrink-0">· {item.count} drifted</span>
              </div>
            ) : (
              <RailRow
                key={item.name}
                name={item.name}
                status={item.pin.status}
                findingCount={countSkillAlertFindings(item.pin)}
                secondary={`${(item.pin.files ?? []).length + 1} ${
                  (item.pin.files ?? []).length === 0 ? 'file' : 'files'
                }${
                  item.pin.last_verified_at
                    ? ` · verified ${formatRelativeTime(new Date(item.pin.last_verified_at))}`
                    : ''
                }`}
                active={item.name === activeName}
                outside={isOutsideFilter(item.name)}
                indent={item.grouped}
                onSelect={onSelect}
              />
            ),
          )}

        {attentionOnly && visibleCount === 0 && (
          <div className="px-3 py-6 text-center space-y-2">
            <CheckCircle2 size={16} className="mx-auto text-status-running/70" />
            <p className="text-[11px] text-text-muted">All clear: no drift or findings.</p>
            <button
              onClick={onToggleAttention}
              className="text-[11px] text-primary hover:underline"
            >
              Show all {totalCount} {totalCount === 1 ? noun : `${noun}s`}
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}

// RailRow is one selectable pin row, shared by both kinds.
function RailRow({
  name,
  status,
  findingCount,
  secondary,
  active,
  outside,
  indent,
  onSelect,
}: {
  name: string;
  status: ServerPins['status'] | SkillPin['status'];
  findingCount: number;
  secondary: string;
  active: boolean;
  outside: boolean;
  indent?: boolean;
  onSelect: (name: string) => void;
}) {
  const { label, colorClass } = pinStatusMeta(status);
  return (
    <button
      onClick={() => onSelect(name)}
      aria-current={active}
      title={outside ? 'Not in the attention filter' : undefined}
      className={cn(
        'w-full flex flex-col gap-0.5 px-3 py-1.5 rounded-md text-left transition-colors',
        active
          ? 'bg-primary/10 text-primary'
          : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
        outside && 'opacity-60',
        indent && 'pl-5',
      )}
    >
      <span className="flex items-center gap-2 w-full">
        <span className={cn('flex-shrink-0', colorClass)}>
          {status === 'drift' ? <LockOpen size={11} /> : <Lock size={11} />}
        </span>
        <span className={cn('flex-1 min-w-0 text-xs font-mono truncate', active && 'text-primary')}>
          {name}
        </span>
        {findingCount > 0 && (
          <span
            className="flex-shrink-0 inline-flex items-center gap-0.5 text-[10px] text-status-pending"
            title={`${findingCount} warn-or-critical finding${findingCount > 1 ? 's' : ''}`}
            aria-label={`${findingCount} finding${findingCount > 1 ? 's' : ''} on ${name}`}
          >
            <ShieldAlert size={9} />
            {findingCount}
          </span>
        )}
        <span
          className={cn(
            'flex-shrink-0 text-[10px] px-1.5 py-0.5 rounded',
            status === 'drift'
              ? 'bg-status-pending/15 text-status-pending'
              : 'bg-surface-elevated text-text-muted',
          )}
        >
          {label}
        </span>
      </span>
      <span className="pl-[19px] text-[10px] text-text-muted/70 truncate">{secondary}</span>
    </button>
  );
}

type SkillRailItem =
  | { type: 'group'; repo: string; count: number }
  | { type: 'row'; name: string; pin: SkillPin; grouped: boolean };

// skillRailItems inserts a source header above runs of two or more drifted
// skills sharing one origin repo (the workspace sorts drifted entries by
// repo, so shared-origin drift is always consecutive). Everything else stays
// a flat row.
function skillRailItems(entries: Array<[string, SkillPin]>): SkillRailItem[] {
  const driftRepoCounts = new Map<string, number>();
  for (const [, pin] of entries) {
    const repo = pin.status === 'drift' ? pin.origin?.repo : undefined;
    if (repo) driftRepoCounts.set(repo, (driftRepoCounts.get(repo) ?? 0) + 1);
  }

  const items: SkillRailItem[] = [];
  let openGroup: string | null = null;
  for (const [name, pin] of entries) {
    const repo = pin.status === 'drift' ? pin.origin?.repo : undefined;
    const grouped = !!repo && (driftRepoCounts.get(repo) ?? 0) >= 2;
    if (grouped && repo !== openGroup) {
      items.push({ type: 'group', repo: repo!, count: driftRepoCounts.get(repo!)! });
      openGroup = repo!;
    }
    if (!grouped) openGroup = null;
    items.push({ type: 'row', name, pin, grouped });
  }
  return items;
}
