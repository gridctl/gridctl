import { describe, it, expect } from 'vitest';
import { summarizeSkillResults, addCounts, syncCountsMessage } from '../lib/skillSync';

describe('summarizeSkillResults', () => {
  it('buckets each outcome by its distinguishing field', () => {
    const counts = summarizeSkillResults([
      { skill: 'a', imported: 1 },
      { skill: 'b', skipped: 'local edits' },
      { skill: 'c', imported: 1, backup: 'SKILL.md.pre-abc' },
      { skill: 'd', error: 'boom' },
      { skill: 'e', imported: 0 },
    ]);
    expect(counts).toEqual({ updated: 1, skipped: 1, overwritten: 1, failed: 1 });
  });

  it('treats undefined results as empty', () => {
    expect(summarizeSkillResults(undefined)).toEqual({ updated: 0, skipped: 0, overwritten: 0, failed: 0 });
  });
});

describe('addCounts', () => {
  it('sums two count buckets', () => {
    expect(
      addCounts(
        { updated: 1, skipped: 2, overwritten: 0, failed: 1 },
        { updated: 3, skipped: 0, overwritten: 1, failed: 0 },
      ),
    ).toEqual({ updated: 4, skipped: 2, overwritten: 1, failed: 1 });
  });
});

describe('syncCountsMessage', () => {
  it('renders only non-zero buckets', () => {
    expect(syncCountsMessage({ updated: 2, skipped: 1, overwritten: 0, failed: 0 })).toBe('2 updated, 1 kept');
    expect(syncCountsMessage({ updated: 0, skipped: 0, overwritten: 3, failed: 1 })).toBe('3 overwritten, 1 failed');
  });

  it('is empty when nothing changed (caller falls back to up-to-date)', () => {
    expect(syncCountsMessage({ updated: 0, skipped: 0, overwritten: 0, failed: 0 })).toBe('');
  });
});
