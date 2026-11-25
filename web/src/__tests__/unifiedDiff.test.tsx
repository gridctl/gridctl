import { describe, expect, it } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { classifyUnifiedDiff } from '../lib/unifiedDiff';
import { MAX_DIFF_LINES } from '../lib/diff';
import { UnifiedDiffView } from '../components/ui/UnifiedDiffView';

describe('classifyUnifiedDiff', () => {
  it('classifies headers, hunks, added, removed, context, and notes', () => {
    const diff = [
      '--- a/SKILL.md',
      '+++ b/SKILL.md',
      '@@ -1,3 +1,3 @@',
      ' keep',
      '-old',
      '--- looks like a header',
      '+new',
      '+++ looks like a header too',
      '\\ No newline at end of file',
      '--- /dev/null',
      '+++ b/notes.md',
      '@@ -0,0 +1 @@',
      '+added',
      '',
    ].join('\n');
    expect(classifyUnifiedDiff(diff).map((l) => l.kind)).toEqual([
      'header',
      'header',
      'hunk',
      'context',
      'removed',
      'removed',
      'added',
      'added',
      'note',
      'header',
      'header',
      'hunk',
      'added',
    ]);
  });

  it('normalizes CRLF line endings', () => {
    expect(classifyUnifiedDiff('@@ -1 +1 @@\r\n-a\r\n+b\r\n').map((l) => l.text)).toEqual(['@@ -1 +1 @@', '-a', '+b']);
  });
});

describe('UnifiedDiffView', () => {
  it('classes added and removed lines and escapes hidden characters', () => {
    render(
      <UnifiedDiffView
        ariaLabel="Context diff"
        diff={'--- canonical\n+++ target\n@@ -1 +1 @@\n-old line\n+new\u200b line\u202e\n'}
      />,
    );
    const pre = screen.getByLabelText('Context diff');
    const added = pre.querySelector('[data-kind="added"]');
    const removed = pre.querySelector('[data-kind="removed"]');
    expect(added).toHaveClass('text-status-running');
    expect(removed).toHaveClass('text-status-error');
    expect(added).toHaveTextContent('+new\\u200b line\\u202e');
    expect(pre.textContent).not.toContain('\u200b');
    expect(pre.textContent).not.toContain('\u202e');
  });

  it('caps rendering at MAX_DIFF_LINES and states how many lines were omitted', () => {
    const body = Array.from({ length: MAX_DIFF_LINES + 5 }, (_, i) => `+l${i}`).join('\n');
    render(<UnifiedDiffView ariaLabel="Big diff" diff={`@@ -0,0 +1,${MAX_DIFF_LINES + 5} @@\n${body}\n`} />);
    const pre = screen.getByLabelText('Big diff');
    expect(pre.querySelectorAll('[data-kind]')).toHaveLength(MAX_DIFF_LINES);
    expect(screen.getByText('6 more lines not shown.')).toBeInTheDocument();
  });

  it('shows no omission line under the cap', () => {
    render(<UnifiedDiffView ariaLabel="Small diff" diff={'@@ -1 +1 @@\n-a\n+b\n'} />);
    expect(screen.queryByText(/not shown/)).not.toBeInTheDocument();
  });
});
