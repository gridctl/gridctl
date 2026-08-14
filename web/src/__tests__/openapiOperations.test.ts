import { describe, it, expect } from 'vitest';
import {
  buildOperationsFilter,
  collectMethods,
  collectTags,
  deriveOperationsMode,
  describeSkipReason,
  formatOperationsCount,
  formatOperationsSummary,
  methodColorClass,
  operationRowLabel,
  operationsBecomingTools,
  selectableOperations,
  selectedOperationIds,
} from '../lib/openapiOperations';
import type { OpenAPIOperation } from '../lib/api';

function op(partial: Partial<OpenAPIOperation> & { operation_id: string }): OpenAPIOperation {
  return {
    tool_name: partial.operation_id,
    method: 'GET',
    path: `/${partial.operation_id}`,
    ...partial,
  };
}

describe('deriveOperationsMode', () => {
  it('reports all when there is no filter', () => {
    expect(deriveOperationsMode(undefined)).toBe('all');
    expect(deriveOperationsMode({})).toBe('all');
  });

  it('reports the mode of a populated filter', () => {
    expect(deriveOperationsMode({ include: ['a'] })).toBe('include');
    expect(deriveOperationsMode({ exclude: ['a'] })).toBe('exclude');
  });

  it('treats an empty list as its own mode, not as all', () => {
    // The operator picked the mode and has not selected yet. What matters is
    // that this empty list never reaches YAML — see buildOperationsFilter.
    expect(deriveOperationsMode({ include: [] })).toBe('include');
    expect(deriveOperationsMode({ exclude: [] })).toBe('exclude');
  });
});

describe('buildOperationsFilter', () => {
  it('writes include and exclude lists in their respective modes', () => {
    expect(buildOperationsFilter('include', ['listPets'])).toEqual({ include: ['listPets'] });
    expect(buildOperationsFilter('exclude', ['deletePet'])).toEqual({ exclude: ['deletePet'] });
  });

  it('writes no filter at all in all mode, even with a selection', () => {
    expect(buildOperationsFilter('all', ['listPets'])).toBeUndefined();
  });

  it('never produces an empty include list', () => {
    // include: [] means "expose everything" to the backend while reading as a
    // whitelist, so it must collapse to no filter instead.
    expect(buildOperationsFilter('include', [])).toBeUndefined();
    expect(buildOperationsFilter('exclude', [])).toBeUndefined();
  });

  it('copies the input so later mutation cannot reach form state', () => {
    const ids = ['listPets'];
    const filter = buildOperationsFilter('include', ids);
    ids.push('getPetById');
    expect(filter).toEqual({ include: ['listPets'] });
  });

  it('preserves raw operationIds verbatim, including unsanitizable characters', () => {
    // The backend filter matches the raw spec value. Normalizing here would
    // produce a filter that silently matches nothing.
    const filter = buildOperationsFilter('include', ['pets.list', 'Get Pet By Id']);
    expect(filter).toEqual({ include: ['pets.list', 'Get Pet By Id'] });
  });
});

describe('selectedOperationIds', () => {
  it('reads whichever list is present', () => {
    expect(selectedOperationIds({ include: ['a', 'b'] })).toEqual(['a', 'b']);
    expect(selectedOperationIds({ exclude: ['c'] })).toEqual(['c']);
    expect(selectedOperationIds(undefined)).toEqual([]);
  });
});

describe('selectableOperations', () => {
  it('drops operations that cannot become tools', () => {
    const usable = selectableOperations([
      op({ operation_id: 'listPets' }),
      op({ operation_id: '', skipped: true, skip_reason: 'no_operation_id' }),
    ]);
    expect(usable).toHaveLength(1);
    expect(usable[0].operation_id).toBe('listPets');
  });
});

describe('formatOperationsCount', () => {
  it('states the outcome in all mode', () => {
    expect(formatOperationsCount('all', 0, 517)).toBe('All 517 operations will become tools');
  });

  it('states the selection in include mode', () => {
    expect(formatOperationsCount('include', 12, 517)).toBe('12 of 517 selected');
  });

  it('states both sides of the split in exclude mode', () => {
    expect(formatOperationsCount('exclude', 12, 517)).toBe('12 of 517 excluded, 505 become tools');
  });

  it('singularizes a one-operation spec', () => {
    expect(formatOperationsCount('all', 0, 1)).toBe('All 1 operation will become tools');
  });
});

describe('formatOperationsSummary', () => {
  it('summarizes each mode when the total is known', () => {
    expect(formatOperationsSummary(undefined, 517)).toBe('All 517');
    expect(formatOperationsSummary({ include: new Array(12).fill('x') }, 517)).toBe('12 of 517 (include)');
    expect(formatOperationsSummary({ exclude: new Array(12).fill('x') }, 517)).toBe(
      '12 of 517 excluded (exclude)',
    );
  });

  it('degrades to a count when no spec was loaded', () => {
    expect(formatOperationsSummary(undefined, null)).toBe('All operations');
    expect(formatOperationsSummary({ include: ['a', 'b'] }, null)).toBe('2 selected (include)');
    expect(formatOperationsSummary({ exclude: ['a'] }, null)).toBe('1 excluded (exclude)');
  });

  it('treats an empty selection as no filter', () => {
    expect(formatOperationsSummary({ include: [] }, 517)).toBe('All 517');
  });
});

describe('operationsBecomingTools', () => {
  const operations = [
    op({ operation_id: 'listPets' }),
    op({ operation_id: 'deletePet', method: 'DELETE' }),
    op({ operation_id: 'listStores' }),
  ];

  it('returns everything in all mode, whatever is selected', () => {
    expect(operationsBecomingTools(operations, 'all', new Set(['listPets']))).toHaveLength(3);
  });

  it('returns the chosen set in include mode', () => {
    const result = operationsBecomingTools(operations, 'include', new Set(['deletePet']));
    expect(result.map((o) => o.operation_id)).toEqual(['deletePet']);
  });

  it('returns the complement in exclude mode', () => {
    const result = operationsBecomingTools(operations, 'exclude', new Set(['deletePet']));
    expect(result.map((o) => o.operation_id)).toEqual(['listPets', 'listStores']);
  });
});

describe('formatOperationsSummary with a destructive count', () => {
  it('appends the DELETE count when there is one', () => {
    expect(formatOperationsSummary({ include: ['a', 'b'] }, 517, 2)).toBe('2 of 517 (include) · 2 DELETE');
    expect(formatOperationsSummary(undefined, 517, 3)).toBe('All 517 · 3 DELETE');
  });

  it('omits it when nothing destructive is exposed', () => {
    expect(formatOperationsSummary({ include: ['a'] }, 517, 0)).toBe('1 of 517 (include)');
    expect(formatOperationsSummary({ include: ['a'] }, 517)).toBe('1 of 517 (include)');
  });
});

describe('methodColorClass', () => {
  it('gives destructive and safe methods different colors', () => {
    expect(methodColorClass('DELETE')).not.toBe(methodColorClass('GET'));
  });

  it('is case-insensitive and falls back for unknown methods', () => {
    expect(methodColorClass('get')).toBe(methodColorClass('GET'));
    expect(methodColorClass('TRACE')).toBe('text-text-muted');
  });
});

describe('operationRowLabel', () => {
  it('composes method, path, and summary', () => {
    const label = operationRowLabel(
      op({ operation_id: 'listPets', method: 'GET', path: '/pets', summary: 'List all pets' }),
    );
    expect(label).toContain('GET');
    expect(label).toContain('/pets');
    expect(label).toContain('List all pets');
  });

  it('omits the separator when there is no summary', () => {
    expect(operationRowLabel(op({ operation_id: 'listPets', method: 'get', path: '/pets' }))).toBe(
      'GET /pets',
    );
  });
});

describe('collectMethods and collectTags', () => {
  const operations = [
    op({ operation_id: 'a', method: 'get', tags: ['pets', 'read'] }),
    op({ operation_id: 'b', method: 'POST', tags: ['pets'] }),
    op({ operation_id: 'c', method: 'GET' }),
  ];

  it('returns distinct sorted methods, normalized to upper case', () => {
    expect(collectMethods(operations)).toEqual(['GET', 'POST']);
  });

  it('returns distinct sorted tags', () => {
    expect(collectTags(operations)).toEqual(['pets', 'read']);
  });
});

describe('describeSkipReason', () => {
  it('maps known reasons to operator-facing copy', () => {
    expect(describeSkipReason('no_operation_id')).toContain('operationId');
    expect(describeSkipReason('unusable_tool_name')).toContain('sanitized');
  });

  it('passes unknown reasons through rather than swallowing them', () => {
    expect(describeSkipReason('brand_new_reason')).toBe('brand_new_reason');
    expect(describeSkipReason(undefined)).toBe('not usable as a tool');
  });
});
