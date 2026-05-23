import { describe, it, expect } from 'vitest';
import {
  parseImport,
  parseVariablesJson,
  isImportableFile,
} from '../lib/parseFile';

describe('parseVariablesJson — legacy map', () => {
  it('imports every pair as a string secret', () => {
    const { entries, ignored } = parseVariablesJson('{"FOO":"bar","BAZ":"qux"}');
    expect(ignored).toHaveLength(0);
    expect(entries).toHaveLength(2);
    expect(entries[0]).toMatchObject({
      key: 'FOO',
      value: 'bar',
      type: 'string',
      isSecret: true,
    });
    expect(entries[0].set).toBeUndefined();
  });

  it('coerces non-string values to strings', () => {
    const { entries } = parseVariablesJson('{"PORT":5432,"DEBUG":true}');
    expect(entries[0]).toMatchObject({ key: 'PORT', value: '5432' });
    expect(entries[1]).toMatchObject({ key: 'DEBUG', value: 'true' });
  });

  it('ignores invalid keys without aborting the rest', () => {
    const { entries, ignored } = parseVariablesJson('{"foo-bar":"x","OK":"y"}');
    expect(entries).toHaveLength(1);
    expect(entries[0].key).toBe('OK');
    expect(ignored).toHaveLength(1);
    expect(ignored[0].reason).toMatch(/invalid key/i);
  });
});

describe('parseVariablesJson — v2 shape', () => {
  it('honors explicit type, is_secret, and set', () => {
    const input = JSON.stringify({
      variables: [
        { key: 'API_KEY', value: 'sk_live', type: 'string', is_secret: true, set: 'prod' },
        { key: 'PORT', value: '8080', type: 'number', is_secret: false },
      ],
    });
    const { entries, ignored } = parseVariablesJson(input);
    expect(ignored).toHaveLength(0);
    expect(entries[0]).toMatchObject({
      key: 'API_KEY',
      value: 'sk_live',
      type: 'string',
      isSecret: true,
      set: 'prod',
    });
    expect(entries[1]).toMatchObject({
      key: 'PORT',
      type: 'number',
      isSecret: false,
    });
  });

  it('defaults to secret when is_secret is absent', () => {
    const { entries } = parseVariablesJson('{"variables":[{"key":"TOK","value":"x"}]}');
    expect(entries[0]).toMatchObject({ isSecret: true, type: 'string' });
  });

  it('tolerates camelCase isSecret', () => {
    const { entries } = parseVariablesJson(
      '{"variables":[{"key":"PUB","value":"x","isSecret":false}]}',
    );
    expect(entries[0].isSecret).toBe(false);
  });

  it('falls back to string for an invalid type', () => {
    const { entries } = parseVariablesJson(
      '{"variables":[{"key":"X","value":"y","type":"bogus"}]}',
    );
    expect(entries[0].type).toBe('string');
  });

  it('ignores entries with invalid keys', () => {
    const { entries, ignored } = parseVariablesJson(
      '{"variables":[{"key":"bad key","value":"y"},{"key":"GOOD","value":"z"}]}',
    );
    expect(entries).toHaveLength(1);
    expect(entries[0].key).toBe('GOOD');
    expect(ignored).toHaveLength(1);
  });
});

describe('parseVariablesJson — malformed input', () => {
  it('reports invalid JSON as a single ignored line', () => {
    const { entries, ignored } = parseVariablesJson('{not valid');
    expect(entries).toHaveLength(0);
    expect(ignored).toHaveLength(1);
    expect(ignored[0].reason).toMatch(/invalid json/i);
  });

  it('rejects a non-object document', () => {
    const { entries, ignored } = parseVariablesJson('[1,2,3]');
    expect(entries).toHaveLength(0);
    expect(ignored[0].reason).toMatch(/expected a json object/i);
  });

  it('returns nothing for empty input', () => {
    expect(parseVariablesJson('   ')).toEqual({ entries: [], ignored: [] });
  });
});

describe('parseImport — content dispatch', () => {
  it('routes KEY=value content to the env parser', () => {
    const { entries } = parseImport('FOO=bar\nBAZ=qux');
    expect(entries).toHaveLength(2);
    expect(entries[0]).toMatchObject({ key: 'FOO', value: 'bar' });
  });

  it('routes brace-leading content to the JSON parser', () => {
    const { entries } = parseImport('{"FOO":"bar"}');
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ key: 'FOO', value: 'bar', isSecret: true });
  });

  it('detects JSON despite leading whitespace', () => {
    const { entries } = parseImport('  \n {"variables":[{"key":"A","value":"b"}]}');
    expect(entries).toHaveLength(1);
    expect(entries[0].key).toBe('A');
  });
});

describe('isImportableFile', () => {
  const file = (name: string, type = '') =>
    new File(['x'], name, { type });

  it('accepts .env, .json, .txt and dotted env files', () => {
    expect(isImportableFile(file('config.env'))).toBe(true);
    expect(isImportableFile(file('config.json', 'application/json'))).toBe(true);
    expect(isImportableFile(file('notes.txt', 'text/plain'))).toBe(true);
    expect(isImportableFile(file('.env'))).toBe(true);
    expect(isImportableFile(file('.env.local'))).toBe(true);
  });

  it('accepts files with an empty or text MIME type', () => {
    expect(isImportableFile(file('secrets', ''))).toBe(true);
    expect(isImportableFile(file('data', 'text/plain'))).toBe(true);
  });

  it('rejects clearly binary files', () => {
    expect(isImportableFile(file('photo.png', 'image/png'))).toBe(false);
    expect(isImportableFile(file('archive.zip', 'application/zip'))).toBe(false);
  });
});
