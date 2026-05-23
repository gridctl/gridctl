import { describe, it, expect } from 'vitest';
import {
  buildAlphabet,
  entropyBits,
  generateSecret,
  type SecretOptions,
} from '../lib/generateSecret';

const ALL: SecretOptions = {
  length: 24,
  upper: true,
  lower: true,
  digits: true,
  symbols: true,
};

describe('buildAlphabet', () => {
  it('includes only the enabled classes', () => {
    expect(buildAlphabet({ ...ALL, lower: false, digits: false, symbols: false })).toBe(
      'ABCDEFGHIJKLMNOPQRSTUVWXYZ',
    );
    expect(buildAlphabet({ ...ALL, upper: false, lower: false, symbols: false })).toBe(
      '0123456789',
    );
  });

  it('is empty when no class is enabled', () => {
    expect(
      buildAlphabet({ length: 8, upper: false, lower: false, digits: false, symbols: false }),
    ).toBe('');
  });
});

describe('entropyBits', () => {
  it('is length × log2(alphabetSize)', () => {
    // 64-char alphabet → 6 bits/char.
    expect(entropyBits(20, 64)).toBeCloseTo(120, 5);
  });

  it('is zero for degenerate inputs', () => {
    expect(entropyBits(0, 64)).toBe(0);
    expect(entropyBits(20, 1)).toBe(0);
    expect(entropyBits(20, 0)).toBe(0);
  });
});

describe('generateSecret', () => {
  it('honors the requested length', () => {
    for (const length of [8, 24, 64]) {
      expect(generateSecret({ ...ALL, length })).toHaveLength(length);
    }
  });

  it('returns an empty string for non-positive length', () => {
    expect(generateSecret({ ...ALL, length: 0 })).toBe('');
  });

  it('throws when no character class is enabled', () => {
    expect(() =>
      generateSecret({ length: 16, upper: false, lower: false, digits: false, symbols: false }),
    ).toThrow(/at least one character class/);
  });

  it('only emits characters from the selected alphabet', () => {
    const opts: SecretOptions = { length: 200, upper: true, lower: false, digits: true, symbols: false };
    const alphabet = new Set(buildAlphabet(opts));
    for (const ch of generateSecret(opts)) {
      expect(alphabet.has(ch)).toBe(true);
    }
  });

  it('produces a different value on each call (overwhelmingly likely)', () => {
    const a = generateSecret(ALL);
    const b = generateSecret(ALL);
    expect(a).not.toBe(b);
  });

  it('distributes characters roughly uniformly (no modulo bias)', () => {
    // 62-char alphabet does not divide 256, so a naive `% len` would bias the
    // first 8 characters. Rejection sampling must keep the distribution flat.
    const opts: SecretOptions = { length: 1, upper: true, lower: true, digits: true, symbols: false };
    const alphabet = buildAlphabet(opts);
    expect(alphabet).toHaveLength(62);

    const counts = new Map<string, number>();
    const samples = 62 * 2000; // ~2000 expected hits per character
    for (let i = 0; i < samples; i++) {
      const ch = generateSecret(opts);
      counts.set(ch, (counts.get(ch) ?? 0) + 1);
    }

    const expected = samples / alphabet.length;
    // Every character should appear, within ±25% of the expected frequency.
    for (const ch of alphabet) {
      const c = counts.get(ch) ?? 0;
      expect(c).toBeGreaterThan(expected * 0.75);
      expect(c).toBeLessThan(expected * 1.25);
    }
  });
});
