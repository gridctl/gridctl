import { useMemo } from 'react';
import { Clock, Lock, LockOpen } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { ServerPins } from '../../lib/api';
import { escapeNonPrintable, shortPinHash } from '../../lib/nonPrintable';
import { formatRelativeTime } from '../../lib/time';
import { pinStatusMeta } from './pinStatus';
import { FindingsList, FindingsSummaryBadge } from './PinFindings';
import { DriftSection } from './PinsDriftSection';

// Scroll target for ?view=tools and ?view=findings (the findings live inside
// the pinned-records table; a findings-only filter arrives separately).
export const TOOLS_SECTION_ID = 'pins-tools-section';

// ---------------------------------------------------------------------------
// Server detail - drift diff (when drifted) + pinned tool records
// ---------------------------------------------------------------------------

export function PinsServerDetail({ name, pins: sp }: { name: string; pins: ServerPins }) {
  const { label, colorClass } = pinStatusMeta(sp.status);
  const toolRecords = useMemo(
    () => Object.values(sp.tools ?? {}).sort((a, b) => a.name.localeCompare(b.name)),
    [sp.tools],
  );
  const hasDrift = sp.status === 'drift';

  return (
    // A drift review earns the wide layout: the per-tool prose|schema split
    // needs line length for both columns. The clean inventory view keeps the
    // narrow cap for readability.
    <div className={cn('px-6 py-4 space-y-4', hasDrift ? 'max-w-7xl' : 'max-w-3xl')}>
      <div className="flex items-center gap-3">
        <h2 className="text-sm font-mono text-text-primary">{name}</h2>
        <span className={cn('flex items-center gap-1.5 text-xs', colorClass)}>
          {hasDrift ? <LockOpen size={11} /> : <Lock size={11} />}
          {label}
        </span>
        <span
          className="flex items-center gap-1 text-[11px] text-text-muted ml-auto"
          title={sp.last_verified_at || undefined}
        >
          <Clock size={10} className="text-text-muted/60" />
          {sp.last_verified_at
            ? `verified ${formatRelativeTime(new Date(sp.last_verified_at))}`
            : 'never verified'}
        </span>
      </div>

      <div className="flex items-center gap-4 text-[11px] text-text-muted">
        <span>
          <span className="text-text-secondary font-medium">{sp.tool_count}</span> tools pinned
        </span>
        {sp.pinned_at && (
          <span title={sp.pinned_at}>first pinned {formatRelativeTime(new Date(sp.pinned_at))}</span>
        )}
      </div>

      {hasDrift && <DriftSection serverName={name} />}

      <section id={TOOLS_SECTION_ID} className="space-y-2 max-w-5xl">
        <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
          Pinned tool records
        </h3>
        <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
          <table className="w-full text-xs border-collapse">
            <thead>
              <tr className="border-b border-border/30">
                <th className="text-left px-3 py-2 text-text-muted font-medium">Tool</th>
                <th className="text-left px-3 py-2 text-text-muted font-medium">Hash</th>
                <th className="text-left px-3 py-2 text-text-muted font-medium">Pinned</th>
              </tr>
            </thead>
            <tbody>
              {toolRecords.map((rec) => (
                <tr key={rec.name} className="border-b border-border/20 last:border-b-0">
                  <td className="px-3 py-2 align-top">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-text-primary">{escapeNonPrintable(rec.name)}</span>
                      <FindingsSummaryBadge findings={rec.findings} />
                    </div>
                    {rec.description && (
                      <div className="text-[10px] text-text-muted mt-0.5 whitespace-pre-wrap break-words">
                        {escapeNonPrintable(rec.description)}
                      </div>
                    )}
                    {rec.findings && rec.findings.length > 0 && (
                      <div className="mt-1.5">
                        <FindingsList findings={rec.findings} />
                      </div>
                    )}
                  </td>
                  <td className="px-3 py-2 align-top font-mono text-text-muted whitespace-nowrap">
                    {shortPinHash(rec.hash)}
                  </td>
                  <td
                    className="px-3 py-2 align-top text-text-muted whitespace-nowrap"
                    title={rec.pinned_at || undefined}
                  >
                    {rec.pinned_at ? formatRelativeTime(new Date(rec.pinned_at)) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}
