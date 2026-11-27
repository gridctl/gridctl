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
function Harness({ logs }: { logs: ParsedLog[] }) {
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  return (
    <LogStream
      logs={logs}
      isLoading={false}
      error={null}
      fontSize={12}
      containerRef={createRef<HTMLDivElement>()}
      expandedKey={expandedKey}
      onToggleExpand={setExpandedKey}
    />
  );
}

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
        wrap
      />,
    );
    const run = screen.getByTestId('log-inline-fields');
    expect(run).toHaveClass('flex-wrap');
    expect(run).not.toHaveClass('overflow-hidden');
    expect(run.parentElement).toHaveClass('flex-wrap');
  });
});
