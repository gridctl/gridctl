import { describe, it, expect } from 'vitest';
import { hasAnyCategory, skillCategory, skillMetaSummary } from '../lib/skillMeta';
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

  // Contract change: a flat dir used to be returned verbatim, which made the
  // skill's own directory name ("docx") read as a category and gave every
  // skill in a flat registry a one-member group. A flat dir now carries no
  // category at all.
  it('returns "" when the dir is flat, since that is the skill, not a group', () => {
    expect(skillCategory('ops')).toBe('');
    expect(skillCategory('docx')).toBe('');
  });

  it('returns "" for an absent or empty dir (matching the old getGroupKey)', () => {
    expect(skillCategory(undefined)).toBe('');
    expect(skillCategory('')).toBe('');
  });

  it('prefers an explicit category in frontmatter metadata over the dir', () => {
    expect(skillCategory('git-workflow/branch-fork', { category: 'vcs' })).toBe('vcs');
    expect(skillCategory('docx', { category: 'documents' })).toBe('documents');
  });

  it('ignores a blank metadata category and falls back to the dir', () => {
    expect(skillCategory('git-workflow/branch-fork', { category: '  ' })).toBe('git-workflow');
    expect(skillCategory('docx', { category: '' })).toBe('');
  });
});

describe('hasAnyCategory', () => {
  it('is false for a wholly flat registry', () => {
    expect(hasAnyCategory([{ dir: 'ops' }, { dir: 'docx' }, {}])).toBe(false);
  });

  it('is true as soon as one skill is nested or declares a category', () => {
    expect(hasAnyCategory([{ dir: 'ops' }, { dir: 'git-workflow/branch-fork' }])).toBe(true);
    expect(hasAnyCategory([{ dir: 'ops', metadata: { category: 'ops-tools' } }])).toBe(true);
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
