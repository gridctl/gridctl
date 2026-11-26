import type { MCPServerStatus, SecurityCheck, SecurityReport } from '../types';

export type SecurityScope = 'gateway' | { server: string };

export interface SecurityReportState {
  report: SecurityReport | null;
  error: string;
  // True while a manual refresh is in flight. The first fetch is pending
  // while there is neither a report nor an error.
  loading: boolean;
}

// The checks a scoped view shows. The section summary and the evidence body
// both use this filter, so their counts agree.
export function scopeSecurityChecks(checks: SecurityCheck[], scope: SecurityScope): SecurityCheck[] {
  return checks.filter((check) => {
    if (scope === 'gateway') {
      return check.subject.kind === 'gateway' || check.subject.kind === 'skill';
    }
    return (
      (check.subject.kind === 'server' && check.subject.name === scope.server)
      || (check.subject.kind === 'replica' && check.subject.name.startsWith(`${scope.server}:`))
    );
  });
}

// Counts and classifications only. A report with no failures is not a
// secure verdict, so the summary never says one.
export function summarizeSecurityChecks(state: SecurityReportState, scope: SecurityScope): string {
  if (!state.report) return state.error ? 'Report unavailable' : 'Loading report';
  const checks = scopeSecurityChecks(state.report.checks ?? [], scope);
  if (checks.length === 0) return 'No checks for this server';
  const count = (outcome: SecurityCheck['outcome']) =>
    checks.filter((check) => check.outcome === outcome).length;
  const fail = count('fail');
  const warn = count('warn');
  const unknown = count('unknown');
  const pass = count('pass');
  const parts = [
    fail > 0 && `${fail} failing`,
    warn > 0 && `${warn} ${warn === 1 ? 'warning' : 'warnings'}`,
    unknown > 0 && `${unknown} unknown`,
    pass > 0 && `${pass} passing`,
  ].filter((part): part is string => Boolean(part));
  return parts.length > 0 ? parts.join(', ') : 'No applicable checks';
}

// Mirrors the classification order of the Execution evidence body.
export function summarizeExecution(server: MCPServerStatus | undefined, resource: boolean): string {
  if (resource) return 'Not covered for resources';
  if (server?.ssh || server?.external || server?.openapi) return 'Externally managed, unverified';
  if (server?.localProcess || server?.execution?.mode === 'local') return 'Unsandboxed local process';
  const replicas = server?.replicas ?? [];
  if (replicas.length === 0) return 'No active replicas';
  const eligible = replicas.filter(
    (replica) => replica.execution?.mode === 'hardened' && replica.execution.eligible,
  ).length;
  return `${eligible} of ${replicas.length} replicas eligible`;
}
