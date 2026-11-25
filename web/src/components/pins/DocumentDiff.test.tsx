import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { DocumentDiff } from './DocumentDiff';
import { MAX_DIFF_LINES } from '../../lib/diff';

const lines = (n: number, last: string) =>
  Array.from({ length: n }, (_, i) => (i === n - 1 ? last : `line-${i}`)).join('\n');

function lineStartingWith(prefix: string) {
  return screen.getByText((_, el) => el?.tagName === 'SPAN' && (el.textContent ?? '').startsWith(prefix));
}

describe('DocumentDiff', () => {
  it('prefixes added, removed, and unchanged lines and counts changes', () => {
    render(<DocumentDiff label="SKILL.md" oldDocument={'a\nb'} newDocument={'a\nc'} />);
    expect(screen.getByText('SKILL.md (2 lines changed)')).toBeInTheDocument();
    expect(lineStartingWith('  a')).toBeInTheDocument();
    expect(lineStartingWith('- b')).toBeInTheDocument();
    expect(lineStartingWith('+ c')).toBeInTheDocument();
  });

  it('escapes non-printable characters in a line and in the label', () => {
    render(<DocumentDiff label={'fr\u202eag'} oldDocument="x" newDocument={'y\u200b'} />);
    expect(screen.getByText('fr\\u202eag (2 lines changed)')).toBeInTheDocument();
    expect(lineStartingWith('+ y\\u200b')).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('\u200b');
    expect(document.body.textContent).not.toContain('\u202e');
  });

  it('reports degraded false for an aligned diff under the cap', () => {
    const onDegraded = vi.fn();
    render(<DocumentDiff label="doc" oldDocument="a" newDocument="b" onDegraded={onDegraded} />);
    expect(onDegraded).toHaveBeenLastCalledWith(false);
  });

  it('reports degraded true past the render cap', () => {
    const onDegraded = vi.fn();
    const big = Array.from({ length: MAX_DIFF_LINES + 5 }, (_, i) => `n${i}`).join('\n');
    render(<DocumentDiff label="doc" oldDocument="" newDocument={big} onDegraded={onDegraded} />);
    expect(onDegraded).toHaveBeenLastCalledWith(true);
    expect(screen.getByText(/^\d+ more lines not shown\.$/)).toBeInTheDocument();
  });

  it('keeps both sides of an oversize fallback and names the full diff command', () => {
    const side = (tag: string) => Array.from({ length: 1500 }, (_, i) => `${tag}${i}`).join('\n');
    render(
      <DocumentDiff label="doc" oldDocument={side('o')} newDocument={side('n')} fullDiffCommand="gridctl skill pins diff x" />,
    );
    expect(lineStartingWith('- o0')).toBeInTheDocument();
    expect(lineStartingWith('+ n0')).toBeInTheDocument();
    expect(screen.getByText(/^1000 more lines not shown/)).toHaveTextContent('gridctl skill pins diff x');
  });

  it('labels an unaligned diff under the cap and reports degraded true', () => {
    const onDegraded = vi.fn();
    render(
      <DocumentDiff
        label="doc"
        oldDocument={lines(501, 'old-end')}
        newDocument={lines(501, 'new-end')}
        onDegraded={onDegraded}
      />,
    );
    expect(onDegraded).toHaveBeenLastCalledWith(true);
    expect(screen.getByText('doc (too large to align)')).toBeInTheDocument();
    expect(screen.queryByText(/lines? changed/)).not.toBeInTheDocument();
    expect(screen.getByText(/too large to compare line by line/)).toBeInTheDocument();
    expect(screen.queryByText(/Diff truncated/)).not.toBeInTheDocument();
  });
});
