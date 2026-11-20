import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { ErrorBoundary, boundaryReload } from '../components/ui/ErrorBoundary';

const CHUNK_URL = 'http://x/assets/LibraryWorkspace-abc.js';
const chunkError = () => new TypeError(`Failed to fetch dynamically imported module: ${CHUNK_URL}`);

function Thrower({ error }: { error: Error }): never {
  throw error;
}

describe('ErrorBoundary', () => {
  const original = boundaryReload.current;
  let reload: ReturnType<typeof vi.fn<() => void>>;

  beforeEach(() => {
    reload = vi.fn<() => void>();
    boundaryReload.current = reload;
    // The boundary logs the caught error; keep test output quiet.
    vi.spyOn(console, 'error').mockImplementation(() => {});
  });

  afterEach(() => {
    boundaryReload.current = original;
    vi.restoreAllMocks();
  });

  for (const variant of ['inline', 'window'] as const) {
    it(`offers Reload for a chunk-load error (${variant})`, () => {
      const { container } = render(
        <ErrorBoundary variant={variant}>
          <Thrower error={chunkError()} />
        </ErrorBoundary>,
      );

      expect(screen.getByRole('heading', { name: 'Reload needed' })).toBeInTheDocument();
      expect(
        screen.getByText('The web UI changed since this tab was loaded. Reload the page to continue.'),
      ).toBeInTheDocument();
      expect(container.textContent).not.toContain(CHUNK_URL);
      expect(container.querySelector(`[title*="${CHUNK_URL}"]`)).not.toBeNull();
      expect(screen.queryByRole('button', { name: 'Try again' })).not.toBeInTheDocument();
      expect(screen.getAllByRole('button')).toHaveLength(1);

      fireEvent.click(screen.getByRole('button', { name: 'Reload' }));
      expect(reload).toHaveBeenCalledTimes(1);
    });
  }

  it('keeps Try again for other errors in the inline variant', () => {
    function Harness() {
      const [broken, setBroken] = useState(true);
      return (
        <>
          <button onClick={() => setBroken(false)}>fix</button>
          <ErrorBoundary variant="inline">
            {broken ? <Thrower error={new Error('boom')} /> : <p>recovered</p>}
          </ErrorBoundary>
        </>
      );
    }
    render(<Harness />);

    expect(screen.getByRole('heading', { name: 'Something went wrong' })).toBeInTheDocument();
    expect(screen.getByText('boom')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'fix' }));
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(screen.getByText('recovered')).toBeInTheDocument();
    expect(reload).not.toHaveBeenCalled();
  });

  it('keeps Reload Window for other errors in the window variant', () => {
    render(
      <ErrorBoundary variant="window">
        <Thrower error={new Error('boom')} />
      </ErrorBoundary>,
    );
    expect(screen.getByRole('heading', { name: 'Something went wrong' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Reload Window' }));
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
