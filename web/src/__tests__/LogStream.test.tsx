import { describe, it, expect, vi } from 'vitest';
import { createRef, useState } from 'react';
import { render, screen, fireEvent, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { LogStream } from '../components/log/LogStream';
import type { ParsedLog } from '../components/log/logTypes';

function entry(over: Partial<ParsedLog>): ParsedLog {
  return {
    level: 'INFO',
    timestamp: '2026-07-24T10:00:00Z',
    message: 'hello',
    raw: 'hello',
    ...over,
  };
}

// Expansion state is lifted out of LogStream (shared with keyboard nav and
// hosts), so tests drive it through a minimal controlled harness.
function Harness({
  logs,
  searchQuery,
  error = null,
}: {
  logs: ParsedLog[];
  searchQuery?: string;
  error?: string | null;
}) {
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const [following, setFollowing] = useState(true);
  return (
    <>
      <div data-testid="following">{String(following)}</div>
      <LogStream
        logs={logs}
        searchQuery={searchQuery}
        isLoading={false}
        error={error}
        hasLoaded
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={expandedKey}
        onToggleExpand={setExpandedKey}
        following={following}
        onFollowingChange={setFollowing}
      />
    </>
  );
}

function makeLogs(n: number, start = 0): ParsedLog[] {
  return Array.from({ length: n }, (_, i) =>
    entry({
      message: `line ${start + i}`,
      raw: `line ${start + i}`,
      timestamp: new Date(Date.UTC(2026, 6, 24, 10, 0, 0) + (start + i) * 1000).toISOString(),
    }),
  );
}

function scroller(): HTMLElement {
  return screen.getByRole('grid', { name: 'Log entries' }).parentElement as HTMLElement;
}

function setMetrics(el: HTMLElement, m: { scrollTop?: number; scrollHeight?: number; clientHeight?: number }) {
  for (const [k, v] of Object.entries(m)) {
    Object.defineProperty(el, k, { configurable: true, writable: true, value: v });
  }
}

// Overflowing container scrolled to the bottom, then up by 300 px.
function scrollUp(el: HTMLElement) {
  setMetrics(el, { scrollHeight: 1000, clientHeight: 200, scrollTop: 800 });
  fireEvent.scroll(el);
  setMetrics(el, { scrollTop: 500 });
  fireEvent.scroll(el);
}

const jumpButton = () => screen.queryByRole('button', { name: /Jump to latest/ });

describe('LogStream follow-tail', () => {
  it('shows Jump to latest after scrolling up in an overflowing list', () => {
    render(<Harness logs={makeLogs(5)} />);
    expect(jumpButton()).toBeNull();
    scrollUp(scroller());
    expect(screen.getByTestId('following')).toHaveTextContent('false');
    expect(jumpButton()).toHaveTextContent(/^Jump to latest$/);
    expect(jumpButton()).toHaveAttribute('title', 'End or G');
  });

  it('does not suspend on a scroll that moves down or stays away from the bottom without rising', () => {
    render(<Harness logs={makeLogs(5)} />);
    const el = scroller();
    setMetrics(el, { scrollHeight: 1000, clientHeight: 200, scrollTop: 300 });
    fireEvent.scroll(el);
    setMetrics(el, { scrollTop: 400 });
    fireEvent.scroll(el);
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(jumpButton()).toBeNull();
  });

  it('counts new entries by identity, singular and plural', () => {
    const base = makeLogs(5);
    const { rerender } = render(<Harness logs={base} />);
    scrollUp(scroller());
    rerender(<Harness logs={[...base, ...makeLogs(1, 5)]} />);
    expect(jumpButton()).toHaveTextContent('1 new · Jump to latest');
    rerender(<Harness logs={[...base, ...makeLogs(3, 5)]} />);
    expect(jumpButton()).toHaveTextContent('3 new · Jump to latest');
  });

  it('keeps counting at a full sliding window and marks overflow once the anchor leaves', () => {
    const { rerender } = render(<Harness logs={makeLogs(5)} />);
    scrollUp(scroller());
    rerender(<Harness logs={makeLogs(5, 2)} />);
    expect(jumpButton()).toHaveTextContent('2 new · Jump to latest');
    rerender(<Harness logs={makeLogs(5, 7)} />);
    expect(jumpButton()).toHaveTextContent('5+ new · Jump to latest');
  });

  it('jumps, resumes, and focuses the grid without scrolling on click', () => {
    const base = makeLogs(5);
    const { rerender } = render(<Harness logs={base} />);
    const el = scroller();
    scrollUp(el);
    rerender(<Harness logs={[...base, ...makeLogs(2, 5)]} />);
    const grid = screen.getByRole('grid', { name: 'Log entries' });
    const focus = vi.spyOn(grid, 'focus');
    fireEvent.click(jumpButton()!);
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(jumpButton()).toBeNull();
    expect(el.scrollTop).toBe(1000);
    expect(focus).toHaveBeenCalledWith({ preventScroll: true });
    expect(document.activeElement).toBe(grid);
  });

  it.each([
    ['End', { key: 'End' }],
    ['G', { key: 'G', shiftKey: true }],
  ])('resumes following on %s', (_name, init) => {
    render(<Harness logs={makeLogs(5)} />);
    const el = scroller();
    scrollUp(el);
    expect(jumpButton()).not.toBeNull();
    fireEvent.keyDown(document, init);
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(jumpButton()).toBeNull();
    expect(el.scrollTop).toBe(1000);
  });

  it('resumes following on the virtualized path past 200 entries', () => {
    render(<Harness logs={makeLogs(250)} />);
    const el = scroller();
    scrollUp(el);
    expect(screen.getByTestId('following')).toHaveTextContent('false');
    const grid = screen.getByRole('grid', { name: 'Log entries' });
    const focus = vi.spyOn(grid, 'focus');
    fireEvent.click(jumpButton()!);
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(jumpButton()).toBeNull();
    expect(focus).toHaveBeenCalledWith({ preventScroll: true });

    scrollUp(el);
    expect(jumpButton()).not.toBeNull();
    fireEvent.keyDown(document, { key: 'End' });
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(jumpButton()).toBeNull();
  });

  it('clears the cursor and keeps the expanded row when resuming', () => {
    render(<Harness logs={makeLogs(5)} />);
    const el = scroller();
    setMetrics(el, { scrollHeight: 1000, clientHeight: 200 });
    fireEvent.keyDown(document, { key: 'j' });
    fireEvent.keyDown(document, { key: 'Enter' });
    expect(screen.getAllByText('line 1')).toHaveLength(2);
    expect(screen.getByTestId('following')).toHaveTextContent('false');
    expect(screen.getByRole('grid')).toHaveAttribute('aria-activedescendant', 'log-row-1');
    fireEvent.keyDown(document, { key: 'End' });
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(screen.getByRole('grid')).not.toHaveAttribute('aria-activedescendant');
    expect(screen.getAllByText('line 1')).toHaveLength(2);
  });

  it('resumes when a scroll lands within 50 px of the bottom', () => {
    render(<Harness logs={makeLogs(5)} />);
    const el = scroller();
    scrollUp(el);
    setMetrics(el, { scrollTop: 760 });
    fireEvent.scroll(el);
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(jumpButton()).toBeNull();
  });

  it('never suspends on j/k in a list that fits its container', () => {
    render(<Harness logs={makeLogs(3)} />);
    fireEvent.keyDown(document, { key: 'j' });
    fireEvent.keyDown(document, { key: 'k' });
    expect(screen.getByTestId('following')).toHaveTextContent('true');
    expect(jumpButton()).toBeNull();
  });

  it('suspends on j/k when the list overflows', () => {
    render(<Harness logs={makeLogs(3)} />);
    setMetrics(scroller(), { scrollHeight: 1000, clientHeight: 200 });
    fireEvent.keyDown(document, { key: 'j' });
    expect(screen.getByTestId('following')).toHaveTextContent('false');
    expect(jumpButton()).not.toBeNull();
  });

  it('shows no button in the error state', () => {
    const { rerender } = render(<Harness logs={makeLogs(5)} />);
    scrollUp(scroller());
    expect(jumpButton()).not.toBeNull();
    rerender(<Harness logs={makeLogs(5)} error="socket closed" />);
    expect(jumpButton()).toBeNull();
  });

  it('ignores End and G while a text input has focus', () => {
    render(
      <>
        <input data-testid="search" />
        <Harness logs={makeLogs(5)} />
      </>,
    );
    scrollUp(scroller());
    const input = screen.getByTestId('search');
    input.focus();
    fireEvent.keyDown(input, { key: 'End' });
    fireEvent.keyDown(input, { key: 'G', shiftKey: true });
    expect(screen.getByTestId('following')).toHaveTextContent('false');
  });
});

describe('LogStream', () => {
  it('renders a failed state and calls onRetry', () => {
    const onRetry = vi.fn();
    render(
      <LogStream
        logs={[]}
        isLoading={false}
        error="socket closed"
        onRetry={onRetry}
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={null}
        onToggleExpand={() => {}}
        following
        onFollowingChange={() => {}}
      />,
    );
    expect(screen.getByText('Logs unavailable')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it('keeps rows on screen when a refresh fails after a completed load', () => {
    render(
      <LogStream
        logs={[entry({ message: 'still here' })]}
        isLoading={false}
        error="socket closed"
        hasLoaded
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={null}
        onToggleExpand={() => {}}
        following
        onFollowingChange={() => {}}
      />,
    );
    expect(screen.getByRole('grid', { name: 'Log entries' })).toBeInTheDocument();
    expect(screen.getByText('still here')).toBeInTheDocument();
    expect(screen.queryByText('Logs unavailable')).not.toBeInTheDocument();
  });

  it('defaults hasLoaded to having rows so direct callers keep their rows on error', () => {
    render(
      <LogStream
        logs={[entry({ message: 'still here' })]}
        isLoading={false}
        error="socket closed"
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={null}
        onToggleExpand={() => {}}
        following
        onFollowingChange={() => {}}
      />,
    );
    expect(screen.getByText('still here')).toBeInTheDocument();
    expect(screen.queryByText('Logs unavailable')).not.toBeInTheDocument();
  });

  it('renders the filter empty state, not the failed state, for a stale empty filter', () => {
    render(
      <LogStream
        logs={[]}
        isLoading={false}
        error="socket closed"
        hasLoaded
        hasActiveFilter
        onClearFilter={() => {}}
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={null}
        onToggleExpand={() => {}}
        following
        onFollowingChange={() => {}}
      />,
    );
    expect(screen.getByText('No entries match your filters')).toBeInTheDocument();
    expect(screen.queryByText('Logs unavailable')).not.toBeInTheDocument();
  });

  it('shows the loading row only before the first load', () => {
    const props = {
      logs: [],
      error: null,
      fontSize: 12,
      containerRef: createRef<HTMLDivElement>(),
      expandedKey: null,
      onToggleExpand: () => {},
      following: true,
      onFollowingChange: () => {},
    };
    const { rerender } = render(<LogStream {...props} isLoading hasLoaded={false} />);
    expect(screen.getByText('Loading logs...')).toBeInTheDocument();
    rerender(<LogStream {...props} isLoading hasLoaded />);
    expect(screen.queryByText('Loading logs...')).not.toBeInTheDocument();
    expect(screen.getByText('No logs yet')).toBeInTheDocument();
  });

  it('keeps the same logical entry expanded when a poll replaces the array', () => {
    const first = entry({ message: 'first entry', timestamp: '2026-07-24T10:00:01Z' });
    const second = entry({ message: 'second entry', timestamp: '2026-07-24T10:00:02Z' });
    const { rerender } = render(<Harness logs={[first, second]} />);

    // Expand the second entry: its message renders twice (row + detail pane).
    fireEvent.click(screen.getByText('second entry'));
    expect(screen.getAllByText('second entry')).toHaveLength(2);

    // Poll tick: fresh array with a new entry prepended shifts every index.
    const newest = entry({ message: 'newest entry', timestamp: '2026-07-24T10:00:03Z' });
    rerender(<Harness logs={[newest, first, second]} />);

    expect(screen.getAllByText('second entry')).toHaveLength(2);
    expect(screen.getAllByText('first entry')).toHaveLength(1);
    expect(screen.getAllByText('newest entry')).toHaveLength(1);
  });

  it('collapses on a second click of the expanded entry', () => {
    const first = entry({ message: 'only entry' });
    render(<Harness logs={[first]} />);

    fireEvent.click(screen.getByText('only entry'));
    expect(screen.getAllByText('only entry')).toHaveLength(2);
    fireEvent.click(screen.getAllByText('only entry')[0]);
    expect(screen.getAllByText('only entry')).toHaveLength(1);
  });

  it('shows copy actions and promoted fields in the expand panel', () => {
    const traced = entry({
      message: 'tool call finished',
      traceId: 'abc123def456',
      attrs: { server: 'github', tool: 'create_issue', client: 'claude-code', custom_key: 'custom-value' },
    });
    render(<Harness logs={[traced]} />);

    fireEvent.click(screen.getByText('tool call finished'));
    expect(screen.getByText('Copy message')).toBeInTheDocument();
    expect(screen.getByText('Copy raw')).toBeInTheDocument();
    expect(screen.getByText('Copy trace ID')).toBeInTheDocument();
    // Promoted slog-flat fields render as labeled rows.
    // Tool and client values also render as row chips, so each appears twice.
    expect(screen.getByText('Tool')).toBeInTheDocument();
    expect(screen.getAllByText('create_issue')).toHaveLength(2);
    expect(screen.getByText('Client')).toBeInTheDocument();
    expect(screen.getAllByText('claude-code')).toHaveLength(2);
    // Non-promoted attrs collapse under "Other attributes" and stay hidden.
    expect(screen.queryByText('custom-value')).not.toBeInTheDocument();
    fireEvent.click(screen.getByText(/Other attributes \(1\)/));
    expect(screen.getByText('custom-value')).toBeInTheDocument();
  });

  it('navigates with j/k and expands with Enter, keyed by entry identity', () => {
    const first = entry({ message: 'nav first', timestamp: '2026-07-24T10:00:01Z' });
    const second = entry({ message: 'nav second', timestamp: '2026-07-24T10:00:02Z' });
    const { rerender } = render(<Harness logs={[first, second]} />);

    // The cursor starts implicitly on the first row; one j moves to the second.
    fireEvent.keyDown(document, { key: 'j' });
    fireEvent.keyDown(document, { key: 'Enter' });
    expect(screen.getAllByText('nav second')).toHaveLength(2);

    // A poll prepending an entry must not teleport the expanded row.
    const newest = entry({ message: 'nav newest', timestamp: '2026-07-24T10:00:03Z' });
    rerender(<Harness logs={[newest, first, second]} />);
    expect(screen.getAllByText('nav second')).toHaveLength(2);

    // Escape collapses the expansion.
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.getAllByText('nav second')).toHaveLength(1);
  });

  it('shows inline chips on a collapsed tool-call row', () => {
    const finished = entry({
      message: 'tool call finished',
      attrs: {
        server: 'github',
        replica_id: 'github-0',
        client: 'claude-code',
        tool: 'create_issue',
        duration: '1.234567ms',
        is_error: false,
      },
    });
    render(<Harness logs={[finished]} />);

    const run = screen.getByTestId('log-inline-fields');
    const chips = run.querySelectorAll('[data-field]');
    expect([...chips].map((c) => c.getAttribute('data-field'))).toEqual([
      'tool',
      'duration',
      'is_error',
      'client',
    ]);
    expect(within(run).getByText('create_issue')).toBeInTheDocument();
    expect(within(run).getByText('1.2ms')).toBeInTheDocument();
    expect(within(run).getByText('ok')).toHaveClass('text-status-running');
    expect(within(run).getByText('claude-code')).toBeInTheDocument();
    expect(within(run).queryByText('github-0')).not.toBeInTheDocument();
    expect(within(run).queryByText('github')).not.toBeInTheDocument();
    expect(chips[0]).toHaveAttribute('title', 'tool=create_issue');
    expect(chips[1]).toHaveAttribute('title', 'duration=1.234567ms');
    // Wrap off: the run and chips clip on one line instead of growing the row.
    expect(run).toHaveClass('overflow-hidden', 'whitespace-nowrap');
    expect(run.parentElement).toHaveClass('overflow-hidden');
    expect(chips[0]).toHaveClass('truncate');
    // Chips are plain text: no buttons or tab stops inside the run.
    expect(within(run).queryAllByRole('button')).toHaveLength(0);
    expect(run.querySelectorAll('[tabindex]')).toHaveLength(0);
  });

  it('shows the error text on a failed row and the output on a stderr row', () => {
    const failed = entry({
      level: 'ERROR',
      message: 'tool call failed',
      timestamp: '2026-07-24T10:00:01Z',
      attrs: { server: 'github', tool: 'search_code', duration: '30s', error: 'context deadline exceeded' },
    });
    const stderr = entry({
      level: 'WARN',
      message: 'server stderr',
      timestamp: '2026-07-24T10:00:02Z',
      attrs: { server: 'py', output: 'Traceback (most recent call last):' },
    });
    render(<Harness logs={[failed, stderr]} />);

    expect(screen.getByText('search_code')).toBeInTheDocument();
    expect(screen.getByText('30.00s')).toBeInTheDocument();
    expect(screen.getByText('context deadline exceeded')).toHaveClass('text-status-error');
    expect(screen.getByText('Traceback (most recent call last):')).toBeInTheDocument();
  });

  it('renders no chip run on rows without listed attributes', () => {
    render(
      <Harness
        logs={[
          entry({ message: 'plain line', timestamp: '2026-07-24T10:00:01Z' }),
          entry({ message: 'unlisted line', timestamp: '2026-07-24T10:00:02Z', attrs: { server: 'x', replica_id: 1, other: 'y' } }),
        ]}
      />,
    );
    expect(screen.queryByTestId('log-inline-fields')).not.toBeInTheDocument();
    const cell = screen.getByText('unlisted line').parentElement!;
    expect(cell).toHaveTextContent(/^unlisted line$/);
    expect(screen.getByText('plain line')).toHaveClass('truncate');
  });

  it('highlights a search match inside a chip value', () => {
    const finished = entry({ message: 'tool call finished', attrs: { tool: 'create_issue' } });
    render(
      <LogStream
        logs={[finished]}
        isLoading={false}
        error={null}
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={null}
        onToggleExpand={() => {}}
        following
        onFollowingChange={() => {}}
        searchQuery="issue"
      />,
    );
    const run = screen.getByTestId('log-inline-fields');
    const mark = run.querySelector('mark');
    expect(mark).toHaveTextContent('issue');
    // Labels are not highlighted.
    expect(within(run).getByText('tool').querySelector('mark')).toBeNull();
    expect(run.querySelectorAll('mark')).toHaveLength(1);
  });

  it('expands the row when a chip is clicked', () => {
    const finished = entry({ message: 'tool call finished', attrs: { tool: 'create_issue' } });
    render(<Harness logs={[finished]} />);
    fireEvent.click(screen.getByText('create_issue'));
    expect(screen.getAllByText('tool call finished')).toHaveLength(2);
    expect(screen.getByText('Copy message')).toBeInTheDocument();
  });

  it('flows chips after the message when wrapping', () => {
    const finished = entry({ message: 'tool call finished', attrs: { tool: 'create_issue' } });
    render(
      <LogStream
        logs={[finished]}
        isLoading={false}
        error={null}
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={null}
        onToggleExpand={() => {}}
        following
        onFollowingChange={() => {}}
        wrap
      />,
    );
    const run = screen.getByTestId('log-inline-fields');
    expect(run).toHaveClass('flex-wrap');
    expect(run).not.toHaveClass('overflow-hidden');
    expect(run.parentElement).toHaveClass('flex-wrap');
  });

  function renderStream(logs: ParsedLog[], extra: Partial<Parameters<typeof LogStream>[0]> = {}) {
    return render(
      <LogStream
        logs={logs}
        isLoading={false}
        error={null}
        fontSize={12}
        containerRef={createRef<HTMLDivElement>()}
        expandedKey={null}
        onToggleExpand={() => {}}
        following
        onFollowingChange={() => {}}
        {...extra}
      />,
    );
  }

  it('renders full level names', () => {
    renderStream([
      entry({ level: 'ERROR', message: 'boom', raw: 'a' }),
      entry({ level: 'DEBUG', message: 'trace', raw: 'b', timestamp: '2026-07-24T10:00:01Z' }),
    ]);
    expect(screen.getByText('ERROR')).toBeInTheDocument();
    expect(screen.getByText('DEBUG')).toBeInTheDocument();
    expect(screen.queryByText('ERRO')).toBeNull();
    expect(screen.queryByText('DEBU')).toBeNull();
  });

  it('highlights a match in the component and source cells', () => {
    renderStream([entry({ component: 'gateway', message: 'listening' })], {
      searchQuery: 'gate',
      showSource: true,
    });
    const marks = Array.from(document.querySelectorAll('mark')).map((m) => m.textContent);
    expect(marks).toEqual(['gate', 'gate']);
    const cells = screen.getAllByTitle('gateway');
    expect(cells).toHaveLength(2);
    for (const cell of cells) expect(cell.querySelector('mark')).toHaveTextContent('gate');
  });

  it('highlights a match inside the source filter button', () => {
    renderStream([entry({ component: 'gateway', message: 'listening' })], {
      searchQuery: 'gate',
      showSource: true,
      onSourceClick: () => {},
    });
    const button = screen.getByRole('button', { name: 'Filter to gateway' });
    expect(button.querySelector('mark')).toHaveTextContent('gate');
    expect(document.querySelectorAll('mark')).toHaveLength(2);
  });

  it('shows a hint when the query matches only a hidden attribute', () => {
    const log = entry({ message: 'request done', attrs: { user_agent: 'claude-code' } });
    const { unmount } = renderStream([log], { searchQuery: 'claude' });
    const hint = screen.getByText('match in attributes');
    expect(hint).toHaveAttribute('title', 'user_agent=claude-code');
    unmount();
    renderStream([log], { searchQuery: 'request' });
    expect(screen.queryByText(/match in/)).toBeNull();
  });

  it('shows a hint for a trace-only match', () => {
    renderStream([entry({ message: 'ok', traceId: 'abc123def' })], { searchQuery: 'abc123' });
    expect(screen.getByText('match in trace ID')).toBeInTheDocument();
  });

  it('marks the trace ID in the expand panel for a trace-only match', () => {
    render(<Harness logs={[entry({ message: 'ok', traceId: 'abc123def' })]} searchQuery="abc123" />);
    fireEvent.click(screen.getByText('ok'));
    const marks = Array.from(document.querySelectorAll('mark')).map((m) => m.textContent);
    expect(marks).toContain('abc123');
  });

  it('opens Other attributes with the match marked when the filter matched there', () => {
    const log = entry({ message: 'request done', attrs: { user_agent: 'claude-code' } });
    render(<Harness logs={[log]} searchQuery="claude" />);
    fireEvent.click(screen.getByText('request done'));
    const toggle = screen.getByRole('button', { name: /Other attributes/ });
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const marks = Array.from(document.querySelectorAll('mark')).map((m) => m.textContent);
    expect(marks).toContain('claude');
  });

  it('keeps Other attributes collapsed when the filter matched elsewhere', () => {
    const log = entry({ message: 'request done', attrs: { user_agent: 'claude-code' } });
    render(<Harness logs={[log]} searchQuery="request" />);
    fireEvent.click(screen.getAllByText('request')[0]);
    expect(screen.getByRole('button', { name: /Other attributes/ })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });

  it('prefixes the date with showDate and keeps time only in relative mode', () => {
    const ts = new Date(2026, 0, 15, 0, 5, 9, 123).toISOString();
    const { unmount } = renderStream([entry({ timestamp: ts })], { showDate: true });
    expect(screen.getByTestId('log-time').textContent).toMatch(/^\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}$/);
    expect(screen.getByTestId('log-time').parentElement!.className).toContain('11.5em');
    unmount();
    renderStream([entry({ timestamp: ts })]);
    expect(screen.getByTestId('log-time')).toHaveTextContent(/^\d{2}:\d{2}:\d{2}\.\d{3}$/);
    expect(screen.getByTestId('log-time').parentElement!.className).toContain('8.5em');
  });
});
