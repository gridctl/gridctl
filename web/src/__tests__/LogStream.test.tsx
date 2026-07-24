import { describe, it, expect } from 'vitest';
import { createRef } from 'react';
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

function streamProps(logs: ParsedLog[]) {
  return {
    logs,
    isLoading: false,
    error: null,
    fontSize: 12,
    containerRef: createRef<HTMLDivElement>(),
  };
}

describe('LogStream', () => {
  it('keeps the same logical entry expanded when a poll replaces the array', () => {
    const first = entry({ message: 'first entry', timestamp: '2026-07-24T10:00:01Z' });
    const second = entry({ message: 'second entry', timestamp: '2026-07-24T10:00:02Z' });
    const { rerender } = render(<LogStream {...streamProps([first, second])} />);

    // Expand the second entry: its message renders twice (row + detail pane).
    fireEvent.click(screen.getByText('second entry'));
    expect(screen.getAllByText('second entry')).toHaveLength(2);

    // Poll tick: fresh array with a new entry prepended shifts every index.
    const newest = entry({ message: 'newest entry', timestamp: '2026-07-24T10:00:03Z' });
    rerender(<LogStream {...streamProps([newest, first, second])} />);

    expect(screen.getAllByText('second entry')).toHaveLength(2);
    expect(screen.getAllByText('first entry')).toHaveLength(1);
    expect(screen.getAllByText('newest entry')).toHaveLength(1);
  });

  it('collapses on a second click of the expanded entry', () => {
    const first = entry({ message: 'only entry' });
    render(<LogStream {...streamProps([first])} />);

    fireEvent.click(screen.getByText('only entry'));
    expect(screen.getAllByText('only entry')).toHaveLength(2);
    fireEvent.click(screen.getAllByText('only entry')[0]);
    expect(screen.getAllByText('only entry')).toHaveLength(1);
  });
});
