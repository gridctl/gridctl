import { describe, expect, it } from 'vitest';
import {
  argsFromFieldValues,
  canonicalJSON,
  childPointer,
  deriveFields,
  fieldValuesFromArgs,
  hasUncheckedKeywords,
  parseJSONNumber,
  topLevelName,
  validateArguments,
} from '../lib/toolSchemaForm';

const SCHEMA = {
  type: 'object',
  properties: {
    query: { type: 'string', description: 'Search text', minLength: 2 },
    limit: { type: 'integer', minimum: 1, maximum: 50, default: 10 },
    ratio: { type: 'number', exclusiveMinimum: 0 },
    mode: { type: 'string', enum: ['fast', 'slow'] },
    verbose: { type: 'boolean' },
    tags: { type: 'array', items: { type: 'string' } },
    choice: { oneOf: [{ type: 'string' }, { type: 'number' }] },
  },
  required: ['query', 'verbose'],
};

describe('deriveFields', () => {
  it('maps each property to a control kind in declared order', () => {
    const model = deriveFields(SCHEMA);
    expect(model.formable).toBe(true);
    expect(model.fields.map((f) => [f.name, f.kind, f.required])).toEqual([
      ['query', 'string', true],
      ['limit', 'integer', false],
      ['ratio', 'number', false],
      ['mode', 'enum', false],
      ['verbose', 'boolean', true],
      ['tags', 'json', false],
      ['choice', 'json', false],
    ]);
    const limit = model.fields[1];
    expect(limit.hasDefault).toBe(true);
    expect(limit.default).toBe(10);
    expect(model.fields[0].description).toBe('Search text');
  });

  it('treats type arrays, missing type, and $ref as JSON fields', () => {
    const model = deriveFields({
      type: 'object',
      properties: { a: { type: ['string', 'null'] }, b: {}, c: { $ref: '#/$defs/x' }, d: 'junk' },
    });
    expect(model.fields.map((f) => f.kind)).toEqual(['json', 'json', 'json', 'json']);
    expect(model.fields[0].typeLabel).toBe('string | null');
  });

  it('tolerates malformed schemas without throwing', () => {
    expect(deriveFields(undefined)).toEqual({ formable: false, fields: [] });
    expect(deriveFields('nope')).toEqual({ formable: false, fields: [] });
    expect(deriveFields({ type: 'object', properties: [] }).formable).toBe(false);
    expect(deriveFields({ type: 'array' }).formable).toBe(false);
    const model = deriveFields({ type: 'object', properties: { a: { type: 'string', enum: 'x' } }, required: 'a' });
    expect(model.fields[0]).toMatchObject({ kind: 'string', required: false });
  });

  it('accepts an object schema with no properties as an empty form', () => {
    expect(deriveFields({ type: 'object' })).toEqual({ formable: true, fields: [] });
    expect(deriveFields({ type: 'object', properties: {} })).toEqual({ formable: true, fields: [] });
  });
});

describe('form values and arguments', () => {
  const model = deriveFields(SCHEMA);

  it('omits unset and empty optional fields', () => {
    const { args, errors } = argsFromFieldValues(model, { ratio: '', mode: '', tags: '  ' });
    expect(args).toEqual({});
    expect(errors).toEqual({});
  });

  it('sends an explicit empty string for a required string field', () => {
    expect(argsFromFieldValues(model, { query: '' }).args).toEqual({ query: '' });
  });

  it('parses numbers, booleans, enums, and JSON', () => {
    const { args, errors } = argsFromFieldValues(model, {
      query: 'hi',
      limit: '5',
      ratio: '0.5',
      mode: '1',
      verbose: 'false',
      tags: '["a"]',
    });
    expect(errors).toEqual({});
    expect(args).toEqual({ query: 'hi', limit: 5, ratio: 0.5, mode: 'slow', verbose: false, tags: ['a'] });
  });

  it('reports values that do not parse instead of sending them', () => {
    const { args, errors } = argsFromFieldValues(model, { limit: 'abc', ratio: '0x10', tags: '[' });
    expect(args).toEqual({});
    expect(errors).toEqual({
      '/limit': 'Must be an integer',
      '/ratio': 'Must be a number',
      '/tags': 'Must be valid JSON',
    });
  });

  it('round-trips arguments and preserves properties the form cannot show', () => {
    const args = { query: 'x', limit: 3, mode: 'fast', verbose: true, tags: ['a'], extra: { n: 1 }, ratio: 'oops' };
    const { values, extra } = fieldValuesFromArgs(model, args);
    expect(extra).toEqual({ extra: { n: 1 }, ratio: 'oops' });
    expect(argsFromFieldValues(model, values, extra).args).toEqual(args);
  });
});

describe('parseJSONNumber', () => {
  it.each(['1', '-2.5', '1e3', '0'])('accepts %s', (t) => expect(parseJSONNumber(t)).not.toBeNull());
  it.each(['1e999', '0x10', '1a', '', 'Infinity', '01', '.5'])('rejects %s', (t) => expect(parseJSONNumber(t)).toBeNull());
});

describe('validateArguments', () => {
  it('reports missing required fields', () => {
    expect(validateArguments(SCHEMA, {}).errors).toEqual({ '/query': 'Required', '/verbose': 'Required' });
  });

  it('checks type, integer, bounds, length, enum, and items', () => {
    const { errors } = validateArguments(SCHEMA, {
      query: 'a',
      verbose: 'yes',
      limit: 1.5,
      ratio: 0,
      mode: 'medium',
      tags: ['ok', 3],
    });
    expect(errors).toEqual({
      '/query': 'Must be at least 2 characters',
      '/verbose': 'Must be true or false',
      '/limit': 'Must be an integer',
      '/ratio': 'Must be greater than 0',
      '/mode': 'Must be one of: "fast", "slow"',
      '/tags/1': 'Must be a string',
    });
  });

  it('checks minimum, maximum, const, pattern, item counts, and nested objects', () => {
    const schema = {
      type: 'object',
      properties: {
        n: { type: 'number', minimum: 1, maximum: 3 },
        m: { type: 'number', exclusiveMaximum: 3 },
        k: { const: 'fixed' },
        p: { type: 'string', pattern: '^[a-z]+$', maxLength: 3 },
        list: { type: 'array', minItems: 1, maxItems: 2 },
        nested: { type: 'object', properties: { inner: { type: 'integer' }, must: {} }, required: ['must'], additionalProperties: false },
      },
      additionalProperties: false,
    };
    expect(validateArguments(schema, { n: 0, m: 3, k: 'other', p: 'ABC', list: [], nested: { inner: 'x', stray: 1 }, more: 1 }).errors).toEqual({
      '/n': 'Must be at least 1',
      '/m': 'Must be less than 3',
      '/k': 'Must equal "fixed"',
      '/p': 'Must match the pattern ^[a-z]+$',
      '/list': 'Must have at least 1 item',
      '/nested/must': 'Required',
      '/nested/inner': 'Must be an integer',
      '/nested/stray': 'Not allowed by the schema',
      '/more': 'Not allowed by the schema',
    });
    expect(validateArguments(schema, { n: 4, p: 'abcd', list: [1, 2, 3] }).errors).toEqual({
      '/n': 'Must be at most 3',
      '/p': 'Must be at most 3 characters',
      '/list': 'Must have at most 2 items',
    });
    expect(validateArguments(schema, { n: 2, m: 2.9, k: 'fixed', p: 'abc', list: [1], nested: { must: 1, inner: 2 } }).errors).toEqual({});
  });

  it('honors draft-4 boolean exclusive bounds', () => {
    const schema = { type: 'object', properties: { n: { type: 'number', minimum: 1, exclusiveMinimum: true } } };
    expect(validateArguments(schema, { n: 1 }).errors).toEqual({ '/n': 'Must be greater than 1' });
  });

  it('accepts a value matching any type in a type array', () => {
    const schema = { type: 'object', properties: { a: { type: ['string', 'null'] } } };
    expect(validateArguments(schema, { a: null }).errors).toEqual({});
    expect(validateArguments(schema, { a: 1 }).errors).toEqual({ '/a': 'Must be a string or null' });
  });

  it('never errors on unsupported keywords and flags them as unchecked', () => {
    const schema = {
      type: 'object',
      properties: {
        a: { oneOf: [{ type: 'string' }, { type: 'number' }] },
        b: { $ref: '#/$defs/b' },
        c: { type: 'string', format: 'email' },
      },
    };
    const result = validateArguments(schema, { a: true, b: 1, c: 'not-an-email' });
    expect(result.errors).toEqual({});
    expect(result.uncheckedKeywords).toBe(true);
  });

  it('treats an invalid pattern as unchecked', () => {
    const schema = { type: 'object', properties: { p: { type: 'string', pattern: '(' } } };
    const result = validateArguments(schema, { p: 'x' });
    expect(result.errors).toEqual({});
    expect(result.uncheckedKeywords).toBe(true);
  });

  it('finds unsupported keywords at depth and ignores supported schemas', () => {
    expect(hasUncheckedKeywords(SCHEMA)).toBe(true);
    expect(hasUncheckedKeywords({ type: 'object', properties: { a: { type: 'array', items: { type: 'object', properties: { b: { anyOf: [] } } } } } })).toBe(true);
    expect(hasUncheckedKeywords({ type: 'object', properties: { a: { type: 'string', minLength: 1 } } })).toBe(false);
    expect(hasUncheckedKeywords({ type: 'object', properties: { a: { type: 'array', items: [{ type: 'string' }] } } })).toBe(true);
  });

  it('tolerates malformed schemas without throwing', () => {
    for (const schema of [undefined, null, 'x', { type: 7 }, { required: 'a', properties: 'b', enum: 'c' }]) {
      expect(() => validateArguments(schema, { a: 1 })).not.toThrow();
    }
  });
});

describe('pointers and canonical JSON', () => {
  it('escapes pointer segments and recovers the top-level name', () => {
    const p = childPointer(childPointer('', 'a/b~c'), 0);
    expect(p).toBe('/a~1b~0c/0');
    expect(topLevelName(p)).toBe('a/b~c');
    expect(topLevelName('')).toBeNull();
  });

  it('sorts object keys', () => {
    expect(canonicalJSON({ b: 1, a: [{ d: 1, c: 2 }] })).toBe('{"a":[{"c":2,"d":1}],"b":1}');
  });
});
