import { describe, it, expect } from 'vitest';
import { schemaParameters } from '../lib/toolSchema';

function rowOf(schema: unknown, name: string) {
  return schemaParameters(schema).rows.find((r) => r.name === name);
}

describe('schemaParameters types', () => {
  it.each([
    ['plain string type', { type: 'string' }, 'string'],
    ['array with items', { type: 'array', items: { type: 'integer' } }, 'array<integer>'],
    ['array without items', { type: 'array' }, 'array'],
    ['union type array', { type: ['string', 'null'] }, 'string | null'],
    ['enum without type', { enum: ['a', 'b'] }, 'enum'],
    ['const without type', { const: 'x' }, 'const'],
    ['anyOf', { anyOf: [{ type: 'string' }, { type: 'number' }, { type: 'string' }] }, 'string | number'],
    ['oneOf', { oneOf: [{ type: 'boolean' }, { $ref: '#/$defs/Thing' }] }, 'boolean | Thing'],
    ['$ref', { $ref: '#/definitions/Address' }, 'Address'],
    ['no type', { description: 'anything' }, 'any'],
    ['boolean property schema', true, 'any'],
    ['non-object property schema', 5, 'any'],
  ])('%s', (_label, prop, expected) => {
    expect(rowOf({ type: 'object', properties: { p: prop } }, 'p')?.type).toBe(expected);
  });
});

describe('schemaParameters rows', () => {
  it('keeps declared order, required, description, default, and enum values', () => {
    const result = schemaParameters({
      type: 'object',
      properties: {
        id: { type: 'string', description: 'The id' },
        limit: { type: 'integer', default: 10, enum: [10, 50] },
        flag: { type: 'boolean', default: false },
        zero: { type: 'number', default: 0 },
        nil: { default: null },
        blank: { type: 'string', default: '' },
      },
      required: ['id'],
    });
    expect(result.rows.map((r) => r.name)).toEqual(['id', 'limit', 'flag', 'zero', 'nil', 'blank']);
    expect(result.rows[0]).toEqual({ name: 'id', type: 'string', required: true, description: 'The id' });
    expect(result.rows[1]).toEqual({
      name: 'limit',
      type: 'integer',
      required: false,
      defaultText: '10',
      enumValues: ['10', '50'],
    });
    expect(result.rows[2].defaultText).toBe('false');
    expect(result.rows[3].defaultText).toBe('0');
    expect(result.rows[4].defaultText).toBe('null');
    expect(result.rows[5].defaultText).toBe('""');
    expect(result.notes).toEqual([]);
    expect(result.empty).toBe(false);
  });

  it('ignores a non-array required', () => {
    const result = schemaParameters({ properties: { a: { type: 'string' } }, required: 'a' });
    expect(result.rows[0].required).toBe(false);
  });

  it('notes a nested object property and a $ref property', () => {
    const nested = schemaParameters({
      properties: { addr: { type: 'object', properties: { city: { type: 'string' } } } },
    });
    expect(nested.rows).toHaveLength(1);
    expect(nested.notes).toHaveLength(1);
    expect(nested.notes[0]).toMatch(/raw schema/);

    const ref = schemaParameters({ properties: { addr: { $ref: '#/$defs/Addr' } } });
    expect(ref.notes).toHaveLength(1);
  });
});

describe('schemaParameters notes', () => {
  it.each([
    ['top-level oneOf', { oneOf: [{ type: 'object' }, { type: 'object' }] }],
    ['top-level anyOf', { anyOf: [{ type: 'object' }] }],
    ['top-level allOf', { allOf: [{ type: 'object' }] }],
    ['top-level $ref', { $ref: '#/$defs/Input' }],
    ['additionalProperties: true', { type: 'object', additionalProperties: true }],
    ['additionalProperties schema', { type: 'object', additionalProperties: { type: 'string' } }],
  ])('%s adds a note pointing to the raw schema', (_label, schema) => {
    const result = schemaParameters(schema);
    expect(result.notes).toHaveLength(1);
    expect(result.notes[0]).toMatch(/see the raw schema/);
    expect(result.empty).toBe(false);
  });

  it('does not note additionalProperties: false', () => {
    expect(schemaParameters({ type: 'object', additionalProperties: false }).notes).toEqual([]);
  });
});

describe('schemaParameters empty and malformed', () => {
  it.each([
    ['{}', {}],
    ['type object', { type: 'object' }],
    ['empty properties', { type: 'object', properties: {} }],
  ])('%s is empty', (_label, schema) => {
    expect(schemaParameters(schema)).toEqual({ rows: [], notes: [], empty: true });
  });

  it.each([
    ['undefined', undefined],
    ['null', null],
    ['a string', 'schema'],
    ['an array', [1, 2]],
    ['properties: 5', { properties: 5 }],
    ['type string', { type: 'string' }],
  ])('%s returns no rows and is not empty, without throwing', (_label, schema) => {
    const result = schemaParameters(schema);
    expect(result.rows).toEqual([]);
    expect(result.empty).toBe(false);
  });

  it('renders 200 properties', () => {
    const properties: Record<string, unknown> = {};
    for (let i = 0; i < 200; i++) properties[`p${i}`] = { type: 'string' };
    expect(schemaParameters({ properties }).rows).toHaveLength(200);
  });
});
