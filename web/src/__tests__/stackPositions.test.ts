import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  MAX_SAVED_POSITIONS,
  STACK_LAYOUT_VERSION,
  clearStackPositions,
  loadStackPositions,
  saveStackPositions,
  stackPositionsPrefName,
} from '../lib/stackPositions';
import { prefKey } from '../lib/preferences';

function key(stack: string): string {
  return prefKey(stackPositionsPrefName(stack));
}

function store(stack: string, payload: unknown): void {
  localStorage.setItem(key(stack), typeof payload === 'string' ? payload : JSON.stringify(payload));
}

function payload(overrides: Record<string, unknown> = {}) {
  return {
    v: 1,
    layout: STACK_LAYOUT_VERSION,
    compact: true,
    positions: { 'mcp-a': { x: 10, y: 20 } },
    ...overrides,
  };
}

describe('stackPositions', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('names the preference per stack, including the stackless name', () => {
    expect(stackPositionsPrefName('demo')).toBe('stack-positions:demo');
    expect(stackPositionsPrefName('')).toBe('stack-positions:');
  });

  it('round trips positions for the same stack and compact flag', () => {
    const positions = new Map([
      ['mcp-a', { x: 1, y: 2 }],
      ['client-x', { x: -3.5, y: 4 }],
    ]);
    saveStackPositions('demo', false, positions);
    expect(JSON.parse(localStorage.getItem(key('demo'))!)).toEqual({
      v: 1,
      layout: STACK_LAYOUT_VERSION,
      compact: false,
      positions: { 'mcp-a': { x: 1, y: 2 }, 'client-x': { x: -3.5, y: 4 } },
    });
    expect([...loadStackPositions('demo', false)]).toEqual([...positions]);
  });

  it('stores only ids and coordinates', () => {
    const positions = new Map([['mcp-a', { x: 1, y: 2, extra: 'secret' } as { x: number; y: number }]]);
    saveStackPositions('demo', true, positions);
    expect(localStorage.getItem(key('demo'))).not.toContain('secret');
  });

  it.each([
    ['no value', null],
    ['invalid JSON', '{not json'],
    ['a non-object payload', '[1,2]'],
    ['wrong v', payload({ v: 2 })],
    ['wrong layout', payload({ layout: STACK_LAYOUT_VERSION + 1 })],
    ['mismatched compact', payload({ compact: false })],
    ['positions not an object', payload({ positions: [{ x: 1, y: 2 }] })],
    ['positions null', payload({ positions: null })],
  ])('returns an empty map for %s', (_label, value) => {
    if (value !== null) store('demo', value);
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    expect(loadStackPositions('demo', true).size).toBe(0);
    expect(setItem).not.toHaveBeenCalled();
  });

  it('skips invalid entries and keeps valid ones', () => {
    store(
      'demo',
      payload({
        positions: {
          'mcp-a': { x: 1, y: 2 },
          'mcp-b': { x: '1', y: 2 },
          'mcp-c': { x: 1 },
          'mcp-d': null,
          ['m'.repeat(513)]: { x: 1, y: 1 },
          '': { x: 1, y: 1 },
          'mcp-f': { x: 0, y: -5 },
        },
      }),
    );
    expect(Object.fromEntries(loadStackPositions('demo', true))).toEqual({
      'mcp-a': { x: 1, y: 2 },
      'mcp-f': { x: 0, y: -5 },
    });
  });

  it('skips an overflowing coordinate that parses to Infinity', () => {
    store(
      'demo',
      '{"v":1,"layout":1,"compact":true,"positions":{"mcp-e":{"x":1e400,"y":0},"mcp-a":{"x":1,"y":2}}}',
    );
    expect(Object.fromEntries(loadStackPositions('demo', true))).toEqual({ 'mcp-a': { x: 1, y: 2 } });
  });

  it('keeps the newest MAX_SAVED_POSITIONS entries on save', () => {
    const positions = new Map<string, { x: number; y: number }>();
    for (let i = 0; i < MAX_SAVED_POSITIONS + 20; i++) positions.set(`mcp-${i}`, { x: i, y: i });
    saveStackPositions('demo', true, positions);
    const saved = JSON.parse(localStorage.getItem(key('demo'))!).positions;
    const ids = Object.keys(saved);
    expect(MAX_SAVED_POSITIONS).toBe(500);
    expect(ids).toHaveLength(MAX_SAVED_POSITIONS);
    expect(ids[0]).toBe('mcp-20');
    expect(ids[ids.length - 1]).toBe(`mcp-${MAX_SAVED_POSITIONS + 19}`);
  });

  it('loads at most MAX_SAVED_POSITIONS entries from an oversized payload', () => {
    const positions: Record<string, { x: number; y: number }> = {};
    for (let i = 0; i < MAX_SAVED_POSITIONS + 50; i++) positions[`mcp-${i}`] = { x: i, y: i };
    store('demo', payload({ positions }));
    expect(loadStackPositions('demo', true).size).toBe(MAX_SAVED_POSITIONS);
  });

  it('isolates stacks, and the empty stack name has its own key', () => {
    saveStackPositions('a', true, new Map([['mcp-a', { x: 1, y: 1 }]]));
    expect(loadStackPositions('b', true).size).toBe(0);
    expect(loadStackPositions('', true).size).toBe(0);
    saveStackPositions('', true, new Map([['mcp-z', { x: 2, y: 2 }]]));
    expect(Object.fromEntries(loadStackPositions('', true))).toEqual({ 'mcp-z': { x: 2, y: 2 } });
    expect(Object.fromEntries(loadStackPositions('a', true))).toEqual({ 'mcp-a': { x: 1, y: 1 } });
  });

  it('clearStackPositions and saving an empty map remove the value', () => {
    saveStackPositions('a', true, new Map([['mcp-a', { x: 1, y: 1 }]]));
    clearStackPositions('a');
    expect(localStorage.getItem(key('a'))).toBeNull();

    saveStackPositions('b', true, new Map([['mcp-a', { x: 1, y: 1 }]]));
    saveStackPositions('b', true, new Map());
    expect(localStorage.getItem(key('b'))).toBeNull();
  });

  it('does not throw when storage rejects writes or reads', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota');
    });
    expect(() => saveStackPositions('q', true, new Map([['mcp-a', { x: 1, y: 1 }]]))).not.toThrow();
    // The session copy keeps positions for this page load.
    expect(loadStackPositions('q', true).size).toBe(1);
    clearStackPositions('q');

    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(loadStackPositions('q', true).size).toBe(0);
  });
});
