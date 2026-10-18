import { usePolicy } from '../../hooks/usePolicy';

// PolicySummary is the read-only gateway inspector line for the per-tool
// policy block. It does not edit the block. "not configured" is the omitted
// key, not an empty block.
export function PolicySummary() {
  const { report, error } = usePolicy(true);
  if (error && !report) {
    return <p className="text-[11px] text-text-muted">Policy status is unavailable.</p>;
  }
  if (!report) {
    return <p className="text-[11px] text-text-muted">Loading policy…</p>;
  }
  if (!report.configured) {
    return <p className="text-[11px] text-text-secondary">Policy: not configured</p>;
  }
  const rules = report.rules?.length ?? 0;
  const destructive = report.block_destructive ? 'on' : 'off';
  return (
    <p className="text-[11px] text-text-secondary leading-relaxed">
      Policy: default {report.default || 'allow'}, {rules} {rules === 1 ? 'rule' : 'rules'}, block_destructive {destructive}
    </p>
  );
}
