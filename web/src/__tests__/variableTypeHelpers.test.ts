import { describe, it, expect } from 'vitest';
import {
  validateVariableInput,
  getValuePlaceholder,
} from '../components/vault/variableTypeHelpers';

describe('validateVariableInput', () => {
  it('accepts any string, including empty', () => {
    expect(validateVariableInput('string', '')).toEqual({
      ok: true,
      normalized: '',
    });
    expect(validateVariableInput('string', 'hello')).toEqual({
      ok: true,
      normalized: 'hello',
    });
  });

  it('requires a value for non-string types', () => {
    for (const type of ['json', 'list', 'number', 'bool'] as const) {
      const r = validateVariableInput(type, '');
      expect(r.ok).toBe(false);
    }
  });

  it('passes through valid JSON and rejects invalid JSON', () => {
    expect(validateVariableInput('json', '{"a":1}')).toEqual({
      ok: true,
      normalized: '{"a":1}',
    });
    expect(validateVariableInput('json', '{nope}').ok).toBe(false);
  });

  it('normalizes comma-separated lists to a JSON array', () => {
    expect(validateVariableInput('list', 'a, b, c')).toEqual({
      ok: true,
      normalized: '["a","b","c"]',
    });
    // Empty segments are dropped.
    expect(validateVariableInput('list', 'a,,b,')).toEqual({
      ok: true,
      normalized: '["a","b"]',
    });
  });

  it('validates numbers', () => {
    expect(validateVariableInput('number', '42')).toEqual({
      ok: true,
      normalized: '42',
    });
    expect(validateVariableInput('number', '-1.5').ok).toBe(true);
    expect(validateVariableInput('number', 'abc').ok).toBe(false);
  });

  it('validates the bool token whitelist case-insensitively', () => {
    for (const v of ['true', 'false', 'YES', 'no', 't', 'F', '1', '0']) {
      expect(validateVariableInput('bool', v).ok).toBe(true);
    }
    expect(validateVariableInput('bool', 'maybe').ok).toBe(false);
  });
});

describe('getValuePlaceholder', () => {
  it('varies the string hint by secrecy and is stable for structured types', () => {
    expect(getValuePlaceholder('string', true)).toBe('secret value');
    expect(getValuePlaceholder('string', false)).toBe('plaintext value');
    expect(getValuePlaceholder('list', true)).toBe('item1, item2, item3');
    expect(getValuePlaceholder('list', false)).toBe('item1, item2, item3');
    expect(getValuePlaceholder('bool', true)).toBe('true or false');
  });
});
