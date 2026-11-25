import { describe, expect, it } from 'vitest';
import { packFindingToPinFinding, packFindingsToPinFindings } from '../lib/pinFindings';

describe('packFindingToPinFinding', () => {
  it('maps danger to critical and warning to warn, keeping the wire word as code', () => {
    expect(packFindingToPinFinding({ severity: 'danger', description: 'd', stepId: 'SKILL.md:line-1' })).toEqual({
      code: 'danger',
      severity: 'critical',
      field: 'SKILL.md:line-1',
      message: 'd',
    });
    expect(packFindingToPinFinding({ severity: 'warning', description: 'w' })).toMatchObject({
      code: 'warning',
      severity: 'warn',
      field: '',
    });
  });

  it('falls back to info, a generic code, and a matched-rule message', () => {
    expect(packFindingToPinFinding({ pattern: 'curl.*sh' })).toEqual({
      code: 'finding',
      severity: 'info',
      field: '',
      message: 'matched rule: curl.*sh',
    });
    expect(packFindingToPinFinding({}).message).toBe('flagged content');
  });

  it('never puts the regex pattern in the snippet or decoded slot', () => {
    const adapted = packFindingsToPinFindings([
      { pattern: 'curl.*sh', description: 'piped shell', severity: 'danger' },
      { pattern: 'sudo' },
    ]);
    for (const f of adapted) {
      expect(f).not.toHaveProperty('snippet');
      expect(f).not.toHaveProperty('decoded');
    }
    expect(packFindingsToPinFindings(undefined)).toEqual([]);
  });
});
