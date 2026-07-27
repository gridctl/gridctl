import { describe, it, expect } from 'vitest';
import {
  countByRefFilter,
  filterVariablesByRef,
  isRefFilter,
  matchesRefFilter,
  REF_FILTERS,
} from '../lib/varFilter';
import type { Consumer, Variable } from '../lib/api';

const explicit: Consumer = {
  kind: 'mcp-server',
  name: 'github',
  field: 'env.TOKEN',
};
const setInjected: Consumer = {
  kind: 'secrets-set',
  name: 'dev',
  field: 'secrets.sets',
};
const scopedSetInjected: Consumer = {
  kind: 'secrets-set',
  name: 'dev',
  field: 'secrets.sets',
  target: 'github',
  targetKind: 'mcp-server',
};

const v = (key: string): Variable => ({
  key,
  type: 'string',
  is_secret: true,
});

describe('isRefFilter', () => {
  it('accepts every declared filter id', () => {
    for (const f of REF_FILTERS) expect(isRefFilter(f.id)).toBe(true);
  });

  it('rejects anything else, so a hand-edited URL falls back', () => {
    expect(isRefFilter('orphans')).toBe(false);
    expect(isRefFilter(undefined)).toBe(false);
    expect(isRefFilter(null)).toBe(false);
    expect(isRefFilter(3)).toBe(false);
  });
});

describe('matchesRefFilter', () => {
  it('classifies an explicit reference', () => {
    expect(matchesRefFilter([explicit], 'explicit')).toBe(true);
    expect(matchesRefFilter([explicit], 'set')).toBe(false);
    expect(matchesRefFilter([explicit], 'none')).toBe(false);
  });

  it('classifies a set injection, scoped or not', () => {
    for (const c of [setInjected, scopedSetInjected]) {
      expect(matchesRefFilter([c], 'set')).toBe(true);
      expect(matchesRefFilter([c], 'explicit')).toBe(false);
    }
  });

  it('treats an empty consumer list as unreferenced', () => {
    expect(matchesRefFilter([], 'none')).toBe(true);
    expect(matchesRefFilter([], 'explicit')).toBe(false);
    expect(matchesRefFilter([], 'set')).toBe(false);
  });

  it('reports a key that is both referenced and injected under both', () => {
    const both = [explicit, setInjected];
    expect(matchesRefFilter(both, 'explicit')).toBe(true);
    expect(matchesRefFilter(both, 'set')).toBe(true);
    expect(matchesRefFilter(both, 'none')).toBe(false);
  });

  it('counts non-server sites as explicit references', () => {
    const gateway: Consumer = { kind: 'gateway', field: 'auth.token' };
    expect(matchesRefFilter([gateway], 'explicit')).toBe(true);
  });

  it('passes everything through under "all"', () => {
    expect(matchesRefFilter([], 'all')).toBe(true);
    expect(matchesRefFilter([explicit], 'all')).toBe(true);
  });
});

describe('filterVariablesByRef', () => {
  const variables = [v('A'), v('B'), v('C')];
  const usage: Record<string, Consumer[]> = {
    A: [explicit],
    B: [setInjected],
    // C has no entry at all.
  };

  it('narrows to the selected class', () => {
    expect(
      filterVariablesByRef(variables, 'explicit', usage, true).map((x) => x.key),
    ).toEqual(['A']);
    expect(
      filterVariablesByRef(variables, 'set', usage, true).map((x) => x.key),
    ).toEqual(['B']);
    expect(
      filterVariablesByRef(variables, 'none', usage, true).map((x) => x.key),
    ).toEqual(['C']);
  });

  it('goes inert when usage is unknown rather than calling everything unreferenced', () => {
    expect(filterVariablesByRef(variables, 'none', {}, false)).toHaveLength(3);
    expect(filterVariablesByRef(variables, 'explicit', {}, false)).toHaveLength(
      3,
    );
  });

  it('is a no-op under "all"', () => {
    expect(filterVariablesByRef(variables, 'all', usage, true)).toHaveLength(3);
  });
});

describe('countByRefFilter', () => {
  it('counts each class over the whole list', () => {
    const counts = countByRefFilter(
      [v('A'), v('B'), v('C')],
      { A: [explicit, setInjected], B: [setInjected] },
      true,
    );
    expect(counts).toEqual({ all: 3, explicit: 1, set: 2, none: 1 });
  });

  it('returns null when usage is unknown, so chips can disable', () => {
    expect(countByRefFilter([v('A')], {}, false)).toBeNull();
  });
});
