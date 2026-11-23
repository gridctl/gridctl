import { describe, it, expect } from 'vitest';
import {
  formatAcceptanceCriterion,
  isCanonicalCriterion,
  parseAcceptanceCriterion,
} from '../lib/skillCriteria';

describe('formatAcceptanceCriterion', () => {
  it('writes the canonical GIVEN/WHEN/THEN string', () => {
    expect(formatAcceptanceCriterion('a', 'b', 'c')).toBe('GIVEN a WHEN b THEN c');
  });
});

describe('isCanonicalCriterion', () => {
  it('accepts a string that formats back to itself', () => {
    const raw = 'GIVEN a WHEN b THEN c';
    expect(isCanonicalCriterion(raw)).toBe(true);
    const parsed = parseAcceptanceCriterion(raw);
    expect(formatAcceptanceCriterion(parsed.given, parsed.when, parsed.then)).toBe(raw);
  });

  it.each([
    'Given X, when Y, then Z',
    'Scenario 1: GIVEN a WHEN b THEN c',
    'GIVEN  a WHEN b THEN c',
    'GIVEN a\nWHEN b\nTHEN c',
    'Output is valid JSON',
    'PASS: output is valid JSON',
    'PASS: GIVEN a WHEN b THEN c',
    'FAIL: GIVEN a WHEN b THEN c',
    '',
  ])('rejects %j', (raw) => {
    expect(isCanonicalCriterion(raw)).toBe(false);
  });
});

describe('parseAcceptanceCriterion', () => {
  it('splits a matched string as before', () => {
    expect(parseAcceptanceCriterion('Given X, when Y, then Z')).toEqual({
      given: 'X,',
      when: 'Y,',
      then: 'Z',
      matched: true,
      raw: 'Given X, when Y, then Z',
    });
  });

  it('falls back to the raw string when unmatched', () => {
    expect(parseAcceptanceCriterion('Output is valid JSON')).toEqual({
      given: 'Output is valid JSON',
      when: '',
      then: '',
      matched: false,
      raw: 'Output is valid JSON',
    });
  });
});
