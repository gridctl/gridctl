import { describe, it, expect } from 'vitest';
import { toolExposureSummary } from '../lib/toolExposure';

describe('toolExposureSummary', () => {
  it.each([
    [{ selected: 107, total: 107, hasAllowList: false, dirty: false }, 'All 107 tools exposed (no allow-list)'],
    [{ selected: 1, total: 1, hasAllowList: false, dirty: false }, 'All 1 tool exposed (no allow-list)'],
    [{ selected: 12, total: 107, hasAllowList: true, dirty: false }, '12 of 107 exposed'],
    [{ selected: 107, total: 107, hasAllowList: true, dirty: false }, '107 of 107 exposed'],
    [{ selected: 13, total: 107, hasAllowList: true, dirty: true }, '13 of 107 selected (unsaved)'],
    [{ selected: 5, total: 107, hasAllowList: false, dirty: true }, '5 of 107 selected (unsaved)'],
  ])('%o reads %s', (input, expected) => {
    expect(toolExposureSummary(input)).toBe(expected);
  });
});
