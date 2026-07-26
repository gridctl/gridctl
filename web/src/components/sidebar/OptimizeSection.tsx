import { useState } from 'react';
import { Lightbulb, TrendingDown, ChevronDown, ChevronRight, Info } from 'lucide-react';
import { useOptimize } from '../../hooks/useOptimize';
import { severityClasses, severityIcon } from '../../lib/severity';
import type { OptimizeFinding } from '../../types';

function formatImpact(usd: number): string {
  if (!usd || usd <= 0) return '—';
  if (usd < 0.01) return '<$0.01';
  return `$${usd.toFixed(2)}`;
}

function FindingRow({ finding }: { finding: OptimizeFinding }) {
  const [open, setOpen] = useState(false);
  const Icon = severityIcon[finding.severity] ?? Info;

  return (
    <div className={`rounded-md border ${severityClasses[finding.severity]} px-2.5 py-2 transition-all`}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-start gap-2 text-left"
        aria-expanded={open}
      >
        <Icon size={12} className="mt-0.5 flex-shrink-0" />
        <div className="flex-1 min-w-0">
          <div className="flex items-baseline gap-2">
            <span className="text-xs font-medium text-text-primary truncate">{finding.title}</span>
            {finding.impact_usd_per_week > 0 && (
              <span className="text-[10px] font-mono text-text-secondary tabular-nums whitespace-nowrap">
                {formatImpact(finding.impact_usd_per_week)}/wk
              </span>
            )}
          </div>
          {!open && (
            <p className="text-[10px] text-text-muted line-clamp-2 mt-0.5">{finding.summary}</p>
          )}
        </div>
        <span className="mt-0.5 flex-shrink-0">
          {open ? <ChevronDown size={12} className="text-text-muted" /> : <ChevronRight size={12} className="text-text-muted" />}
        </span>
      </button>

      {open && (
        <div className="mt-2 space-y-2 pt-2 border-t border-border/30">
          <p className="text-[11px] text-text-secondary">{finding.summary}</p>
          {finding.remediation && (
            <pre className="text-[10px] font-mono text-text-secondary bg-background/50 rounded-md px-2 py-1.5 overflow-x-auto whitespace-pre-wrap break-words">
              {finding.remediation}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

export function OptimizeSection() {
  // Same hook contract as the Metrics savings card, so the two surfaces share
  // one implementation and one cadence.
  const { report, error } = useOptimize(true);

  if (error) {
    return (
      <div className="text-xs text-text-muted px-1 py-2">
        Optimize unavailable.
      </div>
    );
  }

  if (!report) {
    return (
      <div className="text-xs text-text-muted px-1 py-2">
        Loading findings…
      </div>
    );
  }

  const findings = report.findings ?? [];

  if (findings.length === 0) {
    return (
      <div className="space-y-2">
        <div className="flex items-center gap-2 text-xs text-text-secondary">
          <TrendingDown size={12} className="text-status-running" />
          <span>No findings — health score {report.health_score}/100</span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between text-[10px] text-text-muted uppercase tracking-wider px-0.5">
        <span className="flex items-center gap-1.5">
          <Lightbulb size={10} className="text-primary" />
          {findings.length} finding{findings.length === 1 ? '' : 's'}
        </span>
        <span className="font-mono tabular-nums">{report.health_score}/100</span>
      </div>
      <div className="space-y-1.5">
        {findings.map((finding) => (
          <FindingRow key={finding.id} finding={finding} />
        ))}
      </div>
    </div>
  );
}
