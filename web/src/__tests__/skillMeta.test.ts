import { describe, it, expect } from 'vitest';
import { skillCategory, skillMetaSummary } from '../lib/skillMeta';
import type { AgentSkill } from '../types';

function skill(overrides: Partial<AgentSkill> = {}): AgentSkill {
  return {
    name: 'test-skill',
    description: '',
    state: 'active',
    dir: 'git-workflow/branch-fork',
    body: '',
    fileCount: 0,
    ...overrides,
  } as AgentSkill;
}

describe('skillCategory', () => {
  it('returns the top-level dir segment', () => {
    expect(skillCategory('git-workflow/branch-fork')).toBe('git-workflow');
  });

  it('returns the whole value when there is no nesting', () => {
    expect(skillCategory('ops')).toBe('ops');
  });

  it('returns "" for an absent or empty dir (matching the old getGroupKey)', () => {
    expect(skillCategory(undefined)).toBe('');
    expect(skillCategory('')).toBe('');
  });
});

describe('skillMetaSummary', () => {
  it('includes pluralized file and criteria counts', () => {
    expect(
      skillMetaSummary(skill({ fileCount: 3, acceptanceCriteria: ['a', 'b', 'c', 'd'] })),
    ).toEqual(['3 files', '4 criteria']);
  });

  it('uses singular forms for a count of one', () => {
    expect(
      skillMetaSummary(skill({ fileCount: 1, acceptanceCriteria: ['only'] })),
    ).toEqual(['1 file', '1 criterion']);
  });

  it('omits zero or absent files and criteria rather than showing "0"', () => {
    expect(skillMetaSummary(skill({ fileCount: 0 }))).toEqual([]);
    expect(skillMetaSummary(skill({ fileCount: 0, acceptanceCriteria: [] }))).toEqual([]);
  });

  it('appends license and compatibility only when present', () => {
    expect(
      skillMetaSummary(skill({ fileCount: 2, license: 'MIT', compatibility: 'Opus 4.7+' })),
    ).toEqual(['2 files', 'MIT', 'Opus 4.7+']);
  });
});
