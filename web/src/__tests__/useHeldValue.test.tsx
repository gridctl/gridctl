import { describe, it, expect } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useHeldValue } from '../hooks/useHeldValue';

type Props = { value: number; hold: boolean; epoch?: number };

function setup(initial: Props) {
  return renderHook(({ value, hold, epoch }: Props) => useHeldValue(value, hold, epoch), {
    initialProps: initial,
  });
}

describe('useHeldValue', () => {
  it('passes the value through when not held', () => {
    const { result, rerender } = setup({ value: 1, hold: false });
    expect(result.current).toBe(1);
    rerender({ value: 2, hold: false });
    expect(result.current).toBe(2);
  });

  it('holds the value captured when the hold began', () => {
    const { result, rerender } = setup({ value: 1, hold: false });
    rerender({ value: 1, hold: true });
    rerender({ value: 2, hold: true });
    expect(result.current).toBe(1);
    rerender({ value: 3, hold: true });
    expect(result.current).toBe(1);
  });

  it('adopts the current value when the epoch changes', () => {
    const { result, rerender } = setup({ value: 1, hold: true, epoch: 0 });
    rerender({ value: 2, hold: true, epoch: 0 });
    expect(result.current).toBe(1);
    rerender({ value: 2, hold: true, epoch: 1 });
    expect(result.current).toBe(2);
    rerender({ value: 3, hold: true, epoch: 1 });
    expect(result.current).toBe(2);
  });

  it('adopts the latest value on release', () => {
    const { result, rerender } = setup({ value: 1, hold: true });
    rerender({ value: 5, hold: true });
    expect(result.current).toBe(1);
    rerender({ value: 5, hold: false });
    expect(result.current).toBe(5);
  });
});
