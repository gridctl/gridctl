import { describe, it, expect, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  usePinsStore,
  skillHasAlertFindings,
  countDriftedSkills,
  countFindingSkills,
  firstDriftedSkill,
  firstFindingsSkill,
  useFirstDriftedSkill,
} from '../stores/usePinsStore';
import type { SkillPin } from '../lib/api';

function pin(status: SkillPin['status'], overrides: Partial<SkillPin> = {}): SkillPin {
  return {
    skill_hash: 's1:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    status,
    ...overrides,
  };
}

afterEach(() => {
  act(() => {
    usePinsStore.setState({ skillPins: null });
  });
});

describe('skill pin selectors', () => {
  it('excludes info findings from alert signals', () => {
    expect(
      skillHasAlertFindings(
        pin('pinned', {
          findings: [
            { code: 'P004', severity: 'info', confidence: 'low', field: 'body', message: 'm' },
          ],
        }),
      ),
    ).toBe(false);
    expect(
      skillHasAlertFindings(
        pin('pinned', {
          findings: [
            { code: 'P001', severity: 'warn', confidence: 'high', field: 'body', message: 'm' },
          ],
        }),
      ),
    ).toBe(true);
  });

  it('counts and orders drifted and finding skills alphabetically', () => {
    const map = {
      zeta: pin('drift'),
      alpha: pin('drift'),
      quiet: pin('pinned'),
      flagged: pin('pinned', {
        findings: [
          { code: 'P001', severity: 'critical', confidence: 'high', field: 'body', message: 'm' },
        ],
      }),
    };
    expect(countDriftedSkills(map)).toBe(2);
    expect(countFindingSkills(map)).toBe(1);
    expect(firstDriftedSkill(map)).toBe('alpha');
    expect(firstFindingsSkill(map)).toBe('flagged');
    expect(countDriftedSkills(null)).toBe(0);
    expect(firstDriftedSkill(null)).toBeNull();
  });

  it('keeps hook results referentially stable across rerenders', () => {
    act(() => {
      usePinsStore.setState({ skillPins: { a: pin('drift') } });
    });
    const { result, rerender } = renderHook(() => useFirstDriftedSkill());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
    expect(first).toBe('a');
  });
});
