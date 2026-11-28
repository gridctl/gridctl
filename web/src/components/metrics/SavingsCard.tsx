import { useId, useState } from 'react';
import { Link } from 'react-router';
import { Lightbulb } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatCompactNumber } from '../../lib/format';
import { severityClasses, severityIcon } from '../../lib/severity';
import { findingTarget, findingToolsHref, impactUnitLabel } from './metricsData';
import { PanelHeader } from './metricsShared';
import type { OptimizeFinding, OptimizeReport } from '../../types';

// Compact list: at most three findings so the Overview stays scannable; the
// overflow control opens the full report in the gateway sidebar's Optimize
// section.
const MAX_FINDINGS = 3;

// Severity-major ordering for the visible top slice.
const SEVERITY_RANK: Record<OptimizeFinding['severity'], number> = { critical: 0, warn: 1, info: 2 };

function FindingItem({
  finding,
  onOpenFinding,
}: {
  finding: OptimizeFinding;
  onOpenFinding: (finding: OptimizeFinding) => void;
}) {
  const [open, setOpen] = useState(false);
  const fixId = useId();
  const Icon = severityIcon[finding.severity];
  const linkable = findingTarget(finding) !== null;
  const impact = finding.impact_tokens_per_week ?? 0;
  const hasImpact = impact > 0;
  const hasRemediation = (finding.remediation ?? '').trim().length > 0;
  const toolsHref = findingToolsHref(finding);

  return (
    <li className="rounded-md px-1.5 py-1.5">
      <div className="flex items-baseline gap-2">
        <span
          className={cn(
            'inline-flex items-center gap-1 flex-shrink-0 rounded border px-1.5 py-0.5 text-3xs uppercase tracking-wider',
            severityClasses[finding.severity],
          )}
        >
          <Icon size={9} aria-hidden="true" />
          {finding.severity}
        </span>
        {linkable ? (
          <button
            type="button"
            onClick={() => onOpenFinding(finding)}
            className="flex-1 min-w-0 break-words text-left text-xs text-text-primary hover:text-primary hover:underline rounded-sm cursor-pointer"
          >
            {finding.title}
          </button>
        ) : (
          <span className="flex-1 min-w-0 break-words text-left text-xs text-text-primary">
            {finding.title}
          </span>
        )}
        {hasImpact && (
          <span className="flex-shrink-0 text-3xs font-mono tabular-nums text-text-secondary">
            {`~${formatCompactNumber(impact)} tok/wk`}
          </span>
        )}
      </div>
      <p className="mt-1 text-2xs text-text-secondary">{finding.summary}</p>
      {hasImpact && (
        <p className="mt-0.5 text-3xs text-text-muted">
          tok/wk: {impactUnitLabel(finding.heuristic)}
        </p>
      )}
      {(hasRemediation || toolsHref) && (
        <div className="mt-1 flex flex-wrap items-center gap-3 text-3xs">
          {hasRemediation && (
            <button
              type="button"
              onClick={() => setOpen((v) => !v)}
              aria-expanded={open}
              aria-controls={open ? fixId : undefined}
              className="font-medium text-primary hover:underline rounded-sm cursor-pointer"
            >
              {open ? 'Hide fix' : 'Show fix'}
            </button>
          )}
          {toolsHref && (
            <Link
              to={toolsHref}
              aria-label={`Review in Tools audit: ${finding.server}`}
              className="font-medium text-primary hover:underline rounded-sm"
            >
              Review in Tools audit
            </Link>
          )}
        </div>
      )}
      {open && hasRemediation && (
        <pre
          id={fixId}
          className="mt-1.5 text-3xs font-mono text-text-secondary bg-background/50 rounded-md px-2 py-1.5 overflow-x-auto whitespace-pre-wrap break-words"
        >
          {finding.remediation}
        </pre>
      )}
    </li>
  );
}

// SavingsCard is the Metrics Overview slice of the optimize report. Each
// actionable finding renders its full summary, an explanation of what its
// weekly token figure estimates, an inline disclosure for its remediation
// snippet, and a Tools audit link for schema-related heuristics. It returns
// null when the report is empty (the LimitsPanel precedent: never an empty
// shell), and collapses to one quiet paragraph when the report carries only
// advisory info findings (need_more_data and friends have no server or tool
// to inspect).
export function SavingsCard({
  report,
  onOpenFinding,
  onOpenAllFindings,
}: {
  report: OptimizeReport | null;
  onOpenFinding: (finding: OptimizeFinding) => void;
  onOpenAllFindings: () => void;
}) {
  const findings = report?.findings ?? [];
  if (findings.length === 0) return null;

  const actionable = findings.filter((f) => f.severity !== 'info');
  if (actionable.length === 0) {
    return (
      <p className="text-2xs text-text-muted">
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
        {top.map((finding) => (
          <FindingItem key={finding.id} finding={finding} onOpenFinding={onOpenFinding} />
        ))}
      </ul>
      {hiddenCount > 0 && (
        <div className="px-3.5 pb-2">
          <button
            type="button"
            onClick={onOpenAllFindings}
            className="text-3xs text-text-muted hover:text-text-primary hover:underline rounded-sm cursor-pointer"
          >
            +{hiddenCount} more in the gateway sidebar&apos;s Optimize section
          </button>
        </div>
      )}
    </PanelHeader>
  );
}
