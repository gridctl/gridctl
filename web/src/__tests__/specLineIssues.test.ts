import { describe, expect, it } from 'vitest';
import { getLineIssues } from '../lib/lineIssues';
import type { ValidationIssue } from '../types';

describe('getLineIssues', () => {
  it('matches issue.line when present and falls back to the field heuristic', () => {
    const issues: ValidationIssue[] = [
      { field: 'mcp-servers[0].headerz', message: 'unknown field "headerz" (line 31)', severity: 'error', line: 31 },
      { field: 'servers[4].name', message: 'legacy', severity: 'warning' },
    ];
    expect(getLineIssues(31, issues).map((issue) => issue.field)).toEqual(['mcp-servers[0].headerz']);
    expect(getLineIssues(4, issues).map((issue) => issue.field)).toEqual(['servers[4].name']);
  });
});
