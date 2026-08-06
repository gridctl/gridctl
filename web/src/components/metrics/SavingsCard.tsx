import { Lightbulb } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatCompactNumber } from '../../lib/format';
import { severityClasses, severityIcon } from '../../lib/severity';
import { findingTarget } from './metricsData';
import { PanelHeader } from './metricsShared';
import type { OptimizeFinding, OptimizeReport } from '../../types';

// Compact strip: at most three findings so the Overview stays scannable; the
// full report lives in the gateway sidebar's Optimize section.
const MAX_FINDINGS = 3;

// Severity-major ordering for the visible top slice.
const SEVERITY_RANK: Record<OptimizeFinding['severity'], number> = { critical: 0, warn: 1, info: 2 };

// SavingsCard is the Metrics Overview slice of the optimize report — the
// handshake between "what is this stack using?" (this page) and "what should
// I change?" (optimize). Self-hides when the report is empty (the LimitsPanel
// precedent: never an empty shell), and collapses to one quiet line when the
// report carries only advisory info findings (need_more_data and friends have
// no server/tool to navigate to).
export function SavingsCard({
  report,
  onOpenFinding,
}: {
  report: OptimizeReport | null;
  onOpenFinding: (finding: OptimizeFinding) => void;
}) {
  const findings = report?.findings ?? [];
  if (findings.length === 0) return null;

  const actionable = findings.filter((f) => f.severity !== 'info');
  if (actionable.length === 0) {
    return (
      <p className="text-[11px] text-text-muted/70 line-clamp-1" title={findings[0].summary}>
        Optimize: {findings[0].summary}
      </p>
    );
  }

  const top = [...actionable]
    .sort((a, b) => SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity])
    .slice(0, MAX_FINDINGS);
  const hiddenCount = actionable.length - top.length;

  return (
    <PanelHeader icon={Lightbulb} label="Optimize findings">
      <ul className="px-2 py-2 space-y-1">
        {top.map((finding) => {
          const Icon = severityIcon[finding.severity];
          const linkable = findingTarget(finding) !== null;
          const row = (
            <>
              <span
                className={cn(
                  'inline-flex items-center gap-1 flex-shrink-0 rounded border px-1.5 py-0.5 text-[9px] uppercase tracking-wider',
                  severityClasses[finding.severity],
                )}
              >
                <Icon size={9} aria-hidden="true" />
                {finding.severity}
              </span>
              <span className="flex-1 min-w-0 truncate text-left text-xs text-text-primary">{finding.title}</span>
              {(finding.impact_tokens_per_week ?? 0) > 0 && (
                <span className="flex-shrink-0 text-[10px] font-mono tabular-nums text-text-secondary">
                  {formatCompactNumber(finding.impact_tokens_per_week ?? 0)} tok/wk
                </span>
              )}
            </>
          );
          return (
            <li key={finding.id}>
              {linkable ? (
                <button
                  type="button"
                  onClick={() => onOpenFinding(finding)}
                  title={finding.summary}
                  className="w-full flex items-center gap-2 rounded-md px-1.5 py-1 hover:bg-surface-highlight/40 transition-colors"
                >
                  {row}
                </button>
              ) : (
                <div title={finding.summary} className="flex items-center gap-2 px-1.5 py-1">
                  {row}
                </div>
              )}
            </li>
          );
        })}
      </ul>
      {/* When the list is capped, say where the rest live. */}
      {hiddenCount > 0 && (
        <p className="px-3.5 pb-2 text-[10px] text-text-muted/70">
          +{hiddenCount} more in the gateway sidebar's Optimize section.
        </p>
      )}
    </PanelHeader>
  );
}
