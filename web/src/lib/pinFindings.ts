import type { PackFinding, PinFinding } from './api';

// maxFindingSeverity picks the highest severity present across a tool's
// poisoning-scan findings, or null when there are none. Lives outside the
// component module so React fast refresh keeps working there.
export function maxFindingSeverity(
  findings: PinFinding[] | undefined,
): PinFinding['severity'] | null {
  if (!findings || findings.length === 0) return null;
  if (findings.some((f) => f.severity === 'critical')) return 'critical';
  if (findings.some((f) => f.severity === 'warn')) return 'warn';
  return 'info';
}

// packFindingToPinFinding maps a pack import scan finding onto the shape
// FindingsList renders. The wire pattern is the scanner's regex source, not
// text found in the pack, so it never lands in snippet, which FindingsList
// renders as unlabeled matched content.
export function packFindingToPinFinding(f: PackFinding): Omit<PinFinding, 'confidence'> {
  const severity: PinFinding['severity'] =
    f.severity === 'danger' ? 'critical' : f.severity === 'warning' ? 'warn' : 'info';
  return {
    code: f.severity || 'finding',
    severity,
    field: f.stepId ?? '',
    message: f.description || (f.pattern ? `matched rule: ${f.pattern}` : 'flagged content'),
  };
}

export function packFindingsToPinFindings(findings: PackFinding[] | undefined): Omit<PinFinding, 'confidence'>[] {
  return (findings ?? []).map(packFindingToPinFinding);
}
