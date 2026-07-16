import { describe, it, expect } from 'vitest';
import { escapeNonPrintable, shortPinHash } from '../lib/nonPrintable';

describe('escapeNonPrintable', () => {
  it('leaves printable text untouched', () => {
    expect(escapeNonPrintable('plain text stays, even with punctuation!')).toBe(
      'plain text stays, even with punctuation!',
    );
  });

  it('escapes common control characters with their named forms', () => {
    expect(escapeNonPrintable('line\nbreak\tand\rreturn')).toBe(
      'line\\nbreak\\tand\\rreturn',
    );
  });

  it('escapes bidi overrides and zero-width characters as unicode escapes', () => {
    expect(escapeNonPrintable('rtl‮override')).toBe('rtl\\u202eoverride');
    expect(escapeNonPrintable('zero​width')).toBe('zero\\u200bwidth');
    expect(escapeNonPrintable('bom﻿here')).toBe('bom\\ufeffhere');
  });

  it('escapes backslash so escape-looking text cannot spoof real escapes', () => {
    expect(escapeNonPrintable('back\\slash')).toBe('back\\\\slash');
    expect(escapeNonPrintable('fake \\u202e text')).toBe('fake \\\\u202e text');
  });

  it('escapes bidi isolates, ALM, and astral tag characters', () => {
    expect(escapeNonPrintable('a\u2066b\u2069c')).toBe('a\\u2066b\\u2069c');
    expect(escapeNonPrintable('x\u061cy')).toBe('x\\u061cy');
    expect(escapeNonPrintable('tag\u{e0041}char')).toBe('tag\\u{e0041}char');
  });

  it('escapes C0 controls', () => {
    expect(escapeNonPrintable('bell')).toBe('bell\\u0007');
  });
});

describe('shortPinHash', () => {
  it('keeps the scheme prefix and truncates to 12 hex chars', () => {
    expect(shortPinHash('h2:947cd68fbf83c18ca75435e6730174418b91fd0e')).toBe('h2:947cd68fbf83');
  });

  it('handles legacy unprefixed hashes', () => {
    expect(shortPinHash('947cd68fbf83c18ca75435e6730174418b91fd0e')).toBe('947cd68fbf83');
  });

  it('passes short values through', () => {
    expect(shortPinHash('h2:short')).toBe('h2:short');
    expect(shortPinHash('')).toBe('');
  });
});
