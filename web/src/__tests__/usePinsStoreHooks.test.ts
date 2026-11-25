import { describe, it, expect, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  usePinsStore,
  hookHasAlertFindings,
  countHookAlertFindings,
  countDriftedHooks,
  firstDriftedHook,
  useDriftedHookCount,
  useFirstDriftedHook,
} from '../stores/usePinsStore';
import type { PinFinding } from '../lib/api';
import type { HookPin } from '../types';

function pin(status: HookPin['status'], overrides: Partial<HookPin> = {}): HookPin {
  return {
    definition_hash: 'h1:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    status,
    ...overrides,
  };
}

function finding(severity: PinFinding['severity']): PinFinding {
  return { code: 'P001', severity, confidence: 'high', field: 'body', message: 'm' };
}

afterEach(() => {
  act(() => {
    usePinsStore.setState({ hookPins: null, hookPinsError: null });
  });
});

describe('hook pin store slice', () => {
  it('clears the error when a fetch lands', () => {
    usePinsStore.getState().setHookPinsError('down');
    expect(usePinsStore.getState().hookPinsError).toBe('down');
    usePinsStore.getState().setHookPins({ a: pin('pinned') });
    expect(usePinsStore.getState().hookPinsError).toBeNull();
    expect(usePinsStore.getState().hookPins).toEqual({ a: pin('pinned') });
  });
});

describe('hook pin selectors', () => {
  it('counts warn-or-critical findings only', () => {
    expect(hookHasAlertFindings(pin('pinned', { findings: [finding('info')] }))).toBe(false);
    expect(hookHasAlertFindings(pin('pinned', { findings: [finding('warn')] }))).toBe(true);
    expect(
      countHookAlertFindings(pin('pinned', { findings: [finding('info'), finding('warn'), finding('critical')] })),
    ).toBe(2);
    expect(countHookAlertFindings(pin('pinned'))).toBe(0);
  });

  it('counts drifted hooks and picks the alphabetically first', () => {
    expect(countDriftedHooks(null)).toBe(0);
    expect(firstDriftedHook(null)).toBeNull();
    const pins = { zeta: pin('drift'), alpha: pin('pinned'), beta: pin('drift') };
    expect(countDriftedHooks(pins)).toBe(2);
    expect(firstDriftedHook(pins)).toBe('beta');
    expect(firstDriftedHook({ a: pin('pinned') })).toBeNull();
  });

  it('returns stable values across unrelated store updates', () => {
    act(() => {
      usePinsStore.setState({ hookPins: { b: pin('drift'), a: pin('drift') } });
    });
    const count = renderHook(() => useDriftedHookCount());
    const first = renderHook(() => useFirstDriftedHook());
    expect(count.result.current).toBe(2);
    expect(first.result.current).toBe('a');
    act(() => {
      usePinsStore.setState({ pinsError: 'unrelated' });
    });
    expect(first.result.current).toBe('a');
    act(() => {
      usePinsStore.setState({ hookPins: {}, pinsError: null });
    });
    expect(count.result.current).toBe(0);
    expect(first.result.current).toBeNull();
  });
});
