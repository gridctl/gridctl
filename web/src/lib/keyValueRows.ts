export interface KeyValueRow {
  id: string;
  key: string;
  value: string;
}

// Rows are cached by the identity of the map they produced. Form data keeps
// its map shape, so a map the editor emitted finds its rows (duplicates and
// blanks included) on the next render, while a map from anywhere else (a
// preset, template, draft, or YAML edit) misses and is rebuilt from its entries.
const rowCache = new WeakMap<object, KeyValueRow[]>();
let nextId = 0;

export function newRow(key = '', value = ''): KeyValueRow {
  nextId += 1;
  return { id: `kv-${nextId}`, key, value };
}

export function rowsForMap(map: Record<string, string>): KeyValueRow[] {
  const cached = rowCache.get(map);
  if (cached) return cached;
  const rows = Object.entries(map).map(([key, value]) => newRow(key, value));
  rowCache.set(map, rows);
  return rows;
}

export function mapFromRows(rows: KeyValueRow[]): Record<string, string> {
  const entries = new Map<string, string>();
  for (const row of rows) {
    if (!entries.has(row.key)) entries.set(row.key, row.value);
  }
  const map: Record<string, string> = Object.fromEntries(entries);
  rowCache.set(map, rows);
  return map;
}

export function duplicateKeys(rows: KeyValueRow[]): Set<string> {
  const seen = new Set<string>();
  const duplicates = new Set<string>();
  for (const row of rows) {
    if (row.key === '') continue;
    if (seen.has(row.key)) duplicates.add(row.key);
    else seen.add(row.key);
  }
  return duplicates;
}
