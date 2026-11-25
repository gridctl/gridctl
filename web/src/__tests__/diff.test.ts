import { describe, it, expect } from 'vitest';
import {
  diffLines,
  diffWords,
  diffSide,
  diffHunks,
  isLineDiffOversized,
  lineDiffExceedsCap,
  lineDiffUnaligned,
  truncateBalanced,
  MAX_DIFF_LINES,
} from '../lib/diff';

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

describe('truncateBalanced', () => {
  const block = (n: number, tag: string) => Array.from({ length: n }, (_, i) => `${tag}-${i}`).join('\n');

  it('keeps short diffs whole', () => {
    const lines = diffLines('a\nb', 'a\nc');
    expect(truncateBalanced(lines)).toEqual({ visible: lines, hidden: 0 });
  });

  it('keeps half of each side of an oversize fallback', () => {
    const lines = diffLines(block(3000, 'old'), block(3000, 'new'));
    const { visible, hidden } = truncateBalanced(lines);
    expect(visible.filter((t) => t.kind === 'removed')).toHaveLength(1000);
    expect(visible.filter((t) => t.kind === 'added')).toHaveLength(1000);
    expect(hidden).toBe(4000);
  });

  it('head-slices interleaved output', () => {
    const lines = Array.from({ length: MAX_DIFF_LINES + 10 }, (_, i) => ({
      kind: i % 2 === 0 ? ('removed' as const) : ('added' as const),
      text: String(i),
    }));
    const { visible, hidden } = truncateBalanced(lines);
    expect(visible).toEqual(lines.slice(0, MAX_DIFF_LINES));
    expect(hidden).toBe(10);
  });
});

describe('lineDiffExceedsCap', () => {
  const block = (n: number, tag: string) => Array.from({ length: n }, (_, i) => `${tag}-${i}`).join('\n');
  const cases: [string, string, string][] = [
    ['small', 'a\nb\nc', 'a\nx\nc'],
    ['mid, aligned under the cap', block(1900, 'a'), block(130, 'a')],
    ['mid, aligned over the cap', block(1900, 'a'), block(130, 'b')],
    ['at the sum boundary', block(1000, 'a'), block(1000, 'b')],
    ['oversize fallback', block(3000, 'a'), block(3000, 'b')],
    ['oversize, one side empty', '', block(2500, 'b')],
  ];
  it.each(cases)('matches diffLines for %s', (_label, a, b) => {
    expect(lineDiffExceedsCap(a, b)).toBe(diffLines(a, b).length > MAX_DIFF_LINES);
  });

  it('runs the alignment when the line counts do not decide it', () => {
    // 1900 x 130 is under the alignment budget but 2030 lines in total.
    expect(lineDiffExceedsCap(block(1900, 'a'), block(130, 'a'))).toBe(false);
    expect(lineDiffExceedsCap(block(1900, 'a'), block(130, 'b'))).toBe(true);
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

describe('diffHunks', () => {
  const lines = (n: number) => Array.from({ length: n }, (_, i) => `l${i}`);

  it('keeps three context lines each side of a single change', () => {
    const old = lines(40);
    const next = [...old];
    next[20] = 'changed';
    const hunks = diffHunks(diffLines(old.join('\n'), next.join('\n')));
    expect(hunks).toHaveLength(1);
    const [hunk] = hunks;
    expect(hunk.map((t) => t.kind)).toEqual(['same', 'same', 'same', 'removed', 'added', 'same', 'same', 'same']);
    expect(hunk[0].text).toBe('l17');
    expect(hunk[hunk.length - 1].text).toBe('l23');
  });

  it('splits two distant changes into two hunks', () => {
    const old = lines(60);
    const next = [...old];
    next[10] = 'x';
    next[40] = 'y';
    expect(diffHunks(diffLines(old.join('\n'), next.join('\n')))).toHaveLength(2);
  });

  it('merges two changes within six lines into one hunk', () => {
    const old = lines(60);
    const next = [...old];
    next[20] = 'x';
    next[26] = 'y';
    const hunks = diffHunks(diffLines(old.join('\n'), next.join('\n')));
    expect(hunks).toHaveLength(1);
    expect(hunks[0][0].text).toBe('l17');
    expect(hunks[0][hunks[0].length - 1].text).toBe('l29');
  });

  it('returns [] when nothing changed', () => {
    expect(diffHunks(diffLines('a\nb\n', 'a\nb\n'))).toEqual([]);
  });
});

describe('isLineDiffOversized', () => {
  const text = (n: number, p: string) => Array.from({ length: n }, (_, i) => `${p}${i}`).join('\n');

  it('is true for two 600-line inputs', () => {
    expect(isLineDiffOversized(text(600, 'a'), text(600, 'b'))).toBe(true);
  });

  it('is false for two 10-line inputs', () => {
    expect(isLineDiffOversized(text(10, 'a'), text(10, 'b'))).toBe(false);
  });
});
