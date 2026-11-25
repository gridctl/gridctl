import { describe, it, expect } from 'vitest';
import { duplicateKeys, mapFromRows, newRow, rowsForMap } from '../lib/keyValueRows';

describe('keyValueRows', () => {
  it('mapFromRows keeps the first value for a repeated key and preserves row order', () => {
    const map = mapFromRows([newRow('B', '1'), newRow('A', '2'), newRow('B', '3')]);
    expect(map).toEqual({ B: '1', A: '2' });
    expect(Object.keys(map)).toEqual(['B', 'A']);
  });

  it('mapFromRows keeps the first blank key and returns {} for no rows', () => {
    expect(mapFromRows([newRow('', 'x'), newRow('', 'y')])).toEqual({ '': 'x' });
    expect(mapFromRows([])).toEqual({});
  });

  it('mapFromRows stores __proto__ as an own key', () => {
    const map = mapFromRows([newRow('__proto__', 'v')]);
    expect(Object.keys(map)).toContain('__proto__');
    expect(Object.getPrototypeOf(map)).toBe(Object.prototype);
  });

  it('rowsForMap returns the rows that produced an emitted map', () => {
    const rows = [newRow('DB', 'postgres'), newRow('DB', 'other'), newRow(), newRow()];
    expect(rowsForMap(mapFromRows(rows))).toBe(rows);
  });

  it('rowsForMap builds one row per entry for an unseen map and reuses them', () => {
    const map = { HOST: 'h', '8080': 'port' };
    const first = rowsForMap(map);
    expect(first.map((row) => [row.key, row.value])).toEqual(Object.entries(map));
    expect(new Set(first.map((row) => row.id)).size).toBe(2);
    expect(rowsForMap(map)).toBe(first);
  });

  it('duplicateKeys returns repeated non-empty keys and ignores blanks', () => {
    const rows = [newRow('A'), newRow('a'), newRow('A'), newRow(), newRow(), newRow('B')];
    expect([...duplicateKeys(rows)]).toEqual(['A']);
  });

  it('an edit by one consumer leaves the shared rows of an unseen map intact', () => {
    const shared = { KEY: 'value' };
    const first = rowsForMap(shared);
    const second = rowsForMap(shared);
    expect(second).toBe(first);
    const edited = first.map((row) => ({ ...row, key: 'OTHER', value: 'changed' }));
    mapFromRows(edited);
    const again = rowsForMap(shared);
    expect(again).toBe(first);
    expect(again.map((row) => [row.key, row.value])).toEqual([['KEY', 'value']]);
  });
});
