import { describe, it, expect } from 'vitest';
import { traceRowPropsEqual, type TraceRowProps } from '../components/traces/TraceRow';
import type { TraceSummary } from '../lib/api';

function trace(over: Partial<TraceSummary> = {}): TraceSummary {
  return {
    traceId: 'abc123def456',
    rootSpanId: 'span1',
    operation: 'tools/call',
    tool: 'list',
    client: 'claude',
    server: 'github',
    startTime: '2026-07-24T10:00:00Z',
    duration: 12,
    spanCount: 3,
    hasError: false,
    status: 'ok',
    ...over,
  };
}

function props(over: Partial<TraceRowProps> = {}): TraceRowProps {
  return {
    trace: trace(),
    relativeLabel: 'just now',
    heat: 0.4,
    isSelected: false,
    isHighlighted: false,
    compact: false,
    onActivate: () => {},
    ...over,
  };
}

describe('traceRowPropsEqual', () => {
  const onActivate = () => {};

  it('treats a fresh trace object with the same rendered fields as equal', () => {
    const prev = props({ onActivate, trace: trace() });
    const next = props({ onActivate, trace: trace({ rootSpanId: 'other', hasError: true }) });
    expect(traceRowPropsEqual(prev, next)).toBe(true);
  });

  it.each([
    ['traceId', trace({ traceId: 'other' })],
    ['tool', trace({ tool: 'other' })],
    ['operation', trace({ operation: 'other' })],
    ['client', trace({ client: 'other' })],
    ['server', trace({ server: 'other' })],
    ['startTime', trace({ startTime: '2026-07-24T11:00:00Z' })],
    ['duration', trace({ duration: 99 })],
    ['spanCount', trace({ spanCount: 9 })],
    ['status', trace({ status: 'error' })],
  ] as const)('differs when %s changes', (_field, nextTrace) => {
    expect(traceRowPropsEqual(props({ onActivate }), props({ onActivate, trace: nextTrace }))).toBe(false);
  });

  it('differs when the relative label, heat, or compact flag changes', () => {
    const base = props({ onActivate });
    expect(traceRowPropsEqual(base, props({ onActivate, relativeLabel: '12s ago' }))).toBe(false);
    expect(traceRowPropsEqual(base, props({ onActivate, heat: 0.9 }))).toBe(false);
    expect(traceRowPropsEqual(base, props({ onActivate, compact: true }))).toBe(false);
  });

  it('differs when selection, highlight, or the activate callback changes', () => {
    const base = props({ onActivate });
    expect(traceRowPropsEqual(base, props({ onActivate, isSelected: true }))).toBe(false);
    expect(traceRowPropsEqual(base, props({ onActivate, isHighlighted: true }))).toBe(false);
    expect(traceRowPropsEqual(base, props({ onActivate: () => {} }))).toBe(false);
  });
});
