import { describe, it, expect } from 'vitest';
import { createRef, useState } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
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
    expect(screen.getByText('Tool')).toBeInTheDocument();
    expect(screen.getByText('create_issue')).toBeInTheDocument();
    expect(screen.getByText('Client')).toBeInTheDocument();
    expect(screen.getByText('claude-code')).toBeInTheDocument();
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
});
