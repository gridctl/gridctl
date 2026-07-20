import type { PinFinding } from './api';

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
