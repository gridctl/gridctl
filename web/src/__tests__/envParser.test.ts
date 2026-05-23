import { describe, it, expect } from 'vitest';
import { parseEnv } from '../lib/envParser';

describe('parseEnv', () => {
  it('parses a basic KEY=value pair', () => {
    const { entries, ignored } = parseEnv('FOO=bar');
    expect(ignored).toHaveLength(0);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      key: 'FOO',
      value: 'bar',
      type: 'string',
      quoted: false,
      line: 1,
    });
  });

  it('handles double-quoted values with spaces and "="', () => {
    const { entries } = parseEnv('TOKEN="a value with = and spaces"');
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({
      key: 'TOKEN',
      value: 'a value with = and spaces',
      quoted: true,
    });
  });

  it('handles single-quoted values literally', () => {
    const { entries } = parseEnv("RAW='no $expansion here'");
    expect(entries[0]).toMatchObject({
      key: 'RAW',
      value: 'no $expansion here',
      quoted: true,
    });
  });

  it('strips inline # comments after unquoted values', () => {
    const { entries } = parseEnv('LEVEL=debug # logging level');
    expect(entries[0]).toMatchObject({ key: 'LEVEL', value: 'debug' });
  });

  it('keeps # inside quoted values', () => {
    const { entries } = parseEnv('HASH="value#with#hashes"');
    expect(entries[0]).toMatchObject({ value: 'value#with#hashes' });
  });

  it('skips full-line comments and blank lines', () => {
    const input = '# top comment\n\nFOO=1\n\n# bottom\n';
    const { entries, ignored } = parseEnv(input);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ key: 'FOO', value: '1', line: 3 });
    expect(ignored).toHaveLength(0);
  });

  it('strips leading "export " from shell-style declarations', () => {
    const { entries } = parseEnv('export DATABASE_URL=postgres://localhost/x');
    expect(entries[0]).toMatchObject({
      key: 'DATABASE_URL',
      value: 'postgres://localhost/x',
    });
  });

  it('reports invalid lines without aborting the parse', () => {
    const input = '!bad-line\nFOO=ok';
    const { entries, ignored } = parseEnv(input);
    expect(entries).toHaveLength(1);
    expect(entries[0]).toMatchObject({ key: 'FOO', value: 'ok' });
    expect(ignored).toHaveLength(1);
    expect(ignored[0]).toMatchObject({ line: 1 });
  });

  it('reports keys with invalid characters', () => {
    const { entries, ignored } = parseEnv('foo-bar=ok\nFOO=ok');
    expect(entries).toHaveLength(1);
    expect(entries[0].key).toBe('FOO');
    expect(ignored).toHaveLength(1);
    expect(ignored[0].reason).toMatch(/invalid key/i);
  });

  it('detects bool type for true/false', () => {
    expect(parseEnv('ENABLED=true').entries[0].type).toBe('bool');
    expect(parseEnv('ENABLED=false').entries[0].type).toBe('bool');
  });

  it('detects number type for numeric values', () => {
    expect(parseEnv('COUNT=42').entries[0].type).toBe('number');
    expect(parseEnv('RATIO=-1.5').entries[0].type).toBe('number');
  });

  it('detects json type for object and array literals', () => {
    expect(parseEnv('CONFIG={"k":1}').entries[0].type).toBe('json');
    expect(parseEnv('LIST=[1,2,3]').entries[0].type).toBe('json');
  });

  it('keeps quoted "true" as a string (no auto bool)', () => {
    expect(parseEnv('PRETEND="true"').entries[0].type).toBe('string');
  });

  it('flags unterminated quoted values', () => {
    const { entries, ignored } = parseEnv('OPEN="never closed');
    expect(entries).toHaveLength(0);
    expect(ignored[0].reason).toMatch(/unterminated/i);
  });
});
