import { describe, expect, it } from 'vitest';
import {
  scopeSecurityChecks,
  summarizeExecution,
  summarizeSecurityChecks,
  type SecurityReportState,
} from '../lib/evidenceSummary';
import type { MCPServerStatus, SecurityCheck, SecurityOutcome, SecurityReport } from '../types';

function check(outcome: SecurityOutcome, name = 'fetch', kind = 'server', id = `${outcome}-${name}-${Math.random()}`): SecurityCheck {
  return {
    id,
    predicate: 'p',
    subject: { kind, name },
    outcome,
    reason_code: 'r',
    explanation: 'e',
    evidence: { basis: 'declared', availability: 'available', freshness: 'unknown' },
  } as SecurityCheck;
}

function loaded(checks: SecurityCheck[]): SecurityReportState {
  return { report: { checks } as SecurityReport, error: '', loading: false };
}

const scope = { server: 'fetch' };
const VERDICTS = /secure|safe|all clear|\bOK\b|✓|✔/i;

describe('summarizeSecurityChecks', () => {
  it('reads Loading report while the first fetch is pending', () => {
    expect(summarizeSecurityChecks({ report: null, error: '', loading: false }, scope)).toBe('Loading report');
  });

  it('reads Report unavailable when the fetch failed', () => {
    expect(
      summarizeSecurityChecks({ report: null, error: 'Security evidence report is unavailable.', loading: false }, scope),
    ).toBe('Report unavailable');
  });

  it('distinguishes a loaded report with no scoped checks from a pending fetch', () => {
    expect(summarizeSecurityChecks(loaded([]), scope)).toBe('No checks for this server');
    expect(summarizeSecurityChecks(loaded([check('fail', 'other')]), scope)).toBe('No checks for this server');
  });

  it('reads No applicable checks when every scoped check is not-applicable', () => {
    expect(summarizeSecurityChecks(loaded([check('not-applicable'), check('not-applicable')]), scope)).toBe(
      'No applicable checks',
    );
  });

  it('lists nonzero counts as failing, warning, unknown, passing', () => {
    const checks = [
      check('pass'), check('unknown'), check('fail'), check('pass'), check('unknown'), check('unknown'),
      check('not-applicable'),
    ];
    expect(summarizeSecurityChecks(loaded(checks), scope)).toBe('1 failing, 3 unknown, 2 passing');
    expect(summarizeSecurityChecks(loaded([check('fail'), check('unknown')]), scope)).toBe('1 failing, 1 unknown');
    expect(summarizeSecurityChecks(loaded([check('warn'), check('warn'), ...Array.from({ length: 4 }, () => check('pass'))]), scope)).toBe(
      '2 warnings, 4 passing',
    );
    expect(summarizeSecurityChecks(loaded([check('warn')]), scope)).toBe('1 warning');
    expect(summarizeSecurityChecks(loaded(Array.from({ length: 5 }, () => check('pass'))), scope)).toBe('5 passing');
  });

  it('counts replica checks for the server and ignores other subjects', () => {
    const checks = [
      check('fail', 'fetch:0', 'replica'),
      check('fail', 'fetcher'),
      check('fail', 'fetcher:0', 'replica'),
      check('pass', 'gateway', 'gateway'),
    ];
    expect(summarizeSecurityChecks(loaded(checks), scope)).toBe('1 failing');
  });

  it('keeps the previous counts during a refresh', () => {
    const state = { ...loaded([check('pass')]), loading: true };
    expect(summarizeSecurityChecks(state, scope)).toBe('1 passing');
  });

  it('never states a verdict', () => {
    const outcomes: SecurityOutcome[] = ['pass', 'fail', 'warn', 'unknown', 'not-applicable'];
    const summaries = [
      summarizeSecurityChecks({ report: null, error: '', loading: false }, scope),
      summarizeSecurityChecks({ report: null, error: 'x', loading: false }, scope),
      summarizeSecurityChecks(loaded([]), scope),
      ...outcomes.map((outcome) => summarizeSecurityChecks(loaded([check(outcome)]), scope)),
      summarizeSecurityChecks(loaded(outcomes.map((outcome) => check(outcome))), scope),
    ];
    for (const summary of summaries) expect(summary).not.toMatch(VERDICTS);
  });
});

describe('scopeSecurityChecks', () => {
  it('scopes the gateway to gateway and skill subjects', () => {
    const checks = [check('pass', 'gateway', 'gateway'), check('pass', 'docs', 'skill'), check('pass', 'fetch')];
    expect(scopeSecurityChecks(checks, 'gateway').map((c) => c.subject.kind)).toEqual(['gateway', 'skill']);
  });
});

describe('summarizeExecution', () => {
  const hardened = (eligible: boolean) => ({
    replicaId: 0,
    healthy: true,
    execution: { mode: 'hardened', eligible },
  });

  it('classifies each execution kind in the body order', () => {
    expect(summarizeExecution(undefined, true)).toBe('Not covered for resources');
    for (const flag of ['ssh', 'external', 'openapi']) {
      expect(summarizeExecution({ name: 's', [flag]: true, localProcess: true } as unknown as MCPServerStatus, false)).toBe(
        'Externally managed, unverified',
      );
    }
    expect(summarizeExecution({ name: 's', localProcess: true } as unknown as MCPServerStatus, false)).toBe(
      'Unsandboxed local process',
    );
    expect(
      summarizeExecution({ name: 's', execution: { mode: 'local' }, replicas: [hardened(true)] } as unknown as MCPServerStatus, false),
    ).toBe('Unsandboxed local process');
    expect(summarizeExecution({ name: 's', replicas: [] } as unknown as MCPServerStatus, false)).toBe('No active replicas');
    expect(summarizeExecution(undefined, false)).toBe('No active replicas');
  });

  it('counts eligible hardened replicas against every replica', () => {
    const server = {
      name: 's',
      replicas: [hardened(true), hardened(false), { replicaId: 2, healthy: true, execution: { mode: 'default', eligible: true } }],
    } as unknown as MCPServerStatus;
    expect(summarizeExecution(server, false)).toBe('1 of 3 replicas eligible');
    expect(summarizeExecution({ name: 's', replicas: [hardened(false)] } as unknown as MCPServerStatus, false)).toBe(
      '0 of 1 replicas eligible',
    );
  });
});
