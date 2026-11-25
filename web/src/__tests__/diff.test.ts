import { describe, it, expect } from 'vitest';
import { diffLines, diffWords, diffSide, lineDiffUnaligned, MAX_DIFF_LINES } from '../lib/diff';

describe('diffLines', () => {
  it('marks changed lines, keeps common ones, and numbers both sides', () => {
    const out = diffLines('a\nb\nc', 'a\nx\nc');
    expect(out).toEqual([
      { kind: 'same', text: 'a', oldLine: 1, newLine: 1 },
      { kind: 'removed', text: 'b', oldLine: 2 },
      { kind: 'added', text: 'x', newLine: 2 },
      { kind: 'same', text: 'c', oldLine: 3, newLine: 3 },
    ]);
  });

  it('falls back to removed/added blocks when the input is oversized', () => {
    const big = Array.from({ length: 600 }, (_, i) => `line-${i}`).join('\n');
    const other = Array.from({ length: 600 }, (_, i) => `other-${i}`).join('\n');
    const out = diffLines(big, other);
    expect(out).toHaveLength(1200);
    expect(out.every((t) => t.kind !== 'same')).toBe(true);
  });

  it('exports a render cap for consumers', () => {
    expect(MAX_DIFF_LINES).toBeGreaterThan(0);
  });
});

describe('lineDiffUnaligned', () => {
  const lines = (n: number, tag: string) =>
    Array.from({ length: n }, (_, i) => (i === 0 ? tag : `line-${i}`)).join('\n');
  // A real fallback emits no 'same' tokens even for nearly equal inputs.
  const fellBack = (a: string, b: string) => diffLines(a, b).every((t) => t.kind !== 'same');

  it('is false at 500 by 500 lines, where diffLines still aligns', () => {
    const a = lines(500, 'old');
    const b = lines(500, 'new');
    expect(lineDiffUnaligned(a, b)).toBe(false);
    expect(fellBack(a, b)).toBe(false);
  });

  it('is true at 501 by 500 lines, where diffLines falls back', () => {
    const a = lines(501, 'old');
    const b = lines(500, 'new');
    expect(lineDiffUnaligned(a, b)).toBe(true);
    expect(fellBack(a, b)).toBe(true);
    expect(diffLines(a, b)).toHaveLength(1001);
  });
});

describe('diffWords', () => {
  it('isolates a one-word change', () => {
    const out = diffWords('use list_enabled_zapier_actions to see', 'use inspect_zapier_actions to see');
    expect(out).not.toBeNull();
    const removed = out!.filter((t) => t.kind === 'removed').map((t) => t.text);
    const added = out!.filter((t) => t.kind === 'added').map((t) => t.text);
    expect(removed).toEqual(['list_enabled_zapier_actions']);
    expect(added).toEqual(['inspect_zapier_actions']);
  });

  it('reconstructs each side exactly, whitespace included', () => {
    const oldText = 'alpha  beta\tgamma\ndelta';
    const newText = 'alpha  BETA\tgamma\nomega end';
    const out = diffWords(oldText, newText);
    expect(out).not.toBeNull();
    const oldSide = diffSide(out!, 'old').map((t) => t.text).join('');
    const newSide = diffSide(out!, 'new').map((t) => t.text).join('');
    expect(oldSide).toBe(oldText);
    expect(newSide).toBe(newText);
  });

  it('returns null when the token product exceeds the DP budget', () => {
    const big = Array.from({ length: 1200 }, (_, i) => `w${i}`).join(' ');
    expect(diffWords(big, big.toUpperCase())).toBeNull();
  });
});
