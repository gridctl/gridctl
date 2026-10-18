import type { ValidationIssue } from '../types';

// getLineIssues matches editor lines to validation issues. A strict-decode
// issue carries line; older issues fall back to a field-index heuristic.
export function getLineIssues(lineNum: number, issues: ValidationIssue[]): ValidationIssue[] {
  return issues.filter((issue) => {
    if (issue.line === lineNum) return true;
    return issue.field.includes(`[${lineNum}]`) || issue.field === `line:${lineNum}`;
  });
}
