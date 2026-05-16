import { describe, it, expect } from 'vitest';
import { tokenizeJSON } from '../tokenize';

function flatten(lines: ReturnType<typeof tokenizeJSON>): string {
  if (!lines) return '';
  return lines.map((line) => line.map((tok) => tok.text).join('')).join('\n');
}

function classesFor(
  lines: ReturnType<typeof tokenizeJSON>,
  text: string,
): string[] {
  if (!lines) return [];
  const classes: string[] = [];
  for (const line of lines) {
    for (const tok of line) {
      if (tok.text === text) classes.push(tok.className);
    }
  }
  return classes;
}

describe('tokenizeJSON', () => {
  it('returns null for malformed JSON', () => {
    expect(tokenizeJSON('not valid {')).toBeNull();
  });

  it('tokenizes an object with key, string, number, boolean, and null values', () => {
    const lines = tokenizeJSON(
      '{"name":"alpha","count":3,"active":true,"parent":null}',
    );
    expect(lines).not.toBeNull();
    const text = flatten(lines);
    expect(text).toBe(
      '{\n  "name": "alpha",\n  "count": 3,\n  "active": true,\n  "parent": null\n}',
    );

    expect(classesFor(lines, '"name"')).toEqual(['text-secondary-light']);
    expect(classesFor(lines, '"alpha"')).toEqual(['text-primary-light']);
    expect(classesFor(lines, '3')).toEqual(['text-primary']);
    expect(classesFor(lines, 'true')).toEqual(['text-tertiary-light']);
    expect(classesFor(lines, 'null')).toEqual(['text-tertiary-light']);
    expect(classesFor(lines, '{')).toEqual(['text-text-muted']);
    expect(classesFor(lines, '}')).toEqual(['text-text-muted']);
    expect(classesFor(lines, ': ')).toHaveLength(4);
  });

  it('tokenizes an array of mixed primitives', () => {
    const lines = tokenizeJSON('[1,"two",false,null]');
    expect(lines).not.toBeNull();
    const text = flatten(lines);
    expect(text).toBe('[\n  1,\n  "two",\n  false,\n  null\n]');
    expect(classesFor(lines, '[')).toEqual(['text-text-muted']);
    expect(classesFor(lines, ']')).toEqual(['text-text-muted']);
    expect(classesFor(lines, '1')).toEqual(['text-primary']);
    expect(classesFor(lines, '"two"')).toEqual(['text-primary-light']);
    expect(classesFor(lines, 'false')).toEqual(['text-tertiary-light']);
  });

  it('renders empty containers inline', () => {
    expect(flatten(tokenizeJSON('{}'))).toBe('{}');
    expect(flatten(tokenizeJSON('[]'))).toBe('[]');
  });

  it('handles a primitive root value', () => {
    const lines = tokenizeJSON('42');
    expect(lines).not.toBeNull();
    expect(flatten(lines)).toBe('42');
    expect(classesFor(lines, '42')).toEqual(['text-primary']);
  });

  it('handles a nested object', () => {
    const lines = tokenizeJSON('{"outer":{"inner":1}}');
    expect(flatten(lines)).toBe(
      '{\n  "outer": {\n    "inner": 1\n  }\n}',
    );
    expect(classesFor(lines, '"outer"')).toEqual(['text-secondary-light']);
    expect(classesFor(lines, '"inner"')).toEqual(['text-secondary-light']);
  });
});
