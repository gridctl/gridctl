import { Component, type ErrorInfo, type ReactNode } from 'react';
import { AlertCircle } from 'lucide-react';
import { isChunkLoadError } from '../../lib/chunkReload';

// Seam over the hard page reload so tests can observe it without jsdom
// navigation errors (same pattern as ResetDialog's pageReload).
// eslint-disable-next-line react-refresh/only-export-components -- test seam; exported for unit tests alongside the boundary
export const boundaryReload = { current: () => window.location.reload() };

interface ErrorBoundaryProps {
  children: ReactNode;
  // 'window' fills the viewport and offers a full reload — for the detached
  // popouts and the last-resort top-level boundary. 'inline' fills its parent
  // container and offers an in-place retry — for the in-shell workspace outlet,
  // so a crashing workspace keeps the surrounding shell (header, nav, status
  // bar) mounted and recoverable.
  variant?: 'window' | 'inline';
  // When this value changes, a tripped boundary resets itself. The shell passes
  // the route path so navigating away from a crashed workspace recovers without
  // a full page reload (an unmounted tree otherwise leaves in-app nav inert).
  resetKey?: unknown;
}

interface ErrorBoundaryState {
  error: Error | null;
}

// ErrorBoundary contains a render-time throw so it degrades to a recoverable
// fallback instead of unmounting the whole React tree to a blank page. It is
// the single implementation behind both the detached popout windows and the
// main app shell.
export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // The fallback only shows the message; log the full error + component
    // stack so a render crash is debuggable from the console.
    console.error('ErrorBoundary caught a render error', error, info.componentStack);
  }

  componentDidUpdate(prev: ErrorBoundaryProps) {
    // Clear the error when the reset key changes (e.g. the route) so the next
    // render attempts the new subtree.
    if (this.state.error && prev.resetKey !== this.props.resetKey) {
      this.setState({ error: null });
    }
  }

  private handleRetry = () => {
    this.setState({ error: null });
  };

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;

    const inline = this.props.variant === 'inline';
    const container = inline
      ? 'h-full w-full bg-background flex items-center justify-center p-8'
      : 'h-screen w-screen bg-background flex items-center justify-center';

    // A failed code-split chunk means the UI changed under this tab (an
    // upgrade or a rebuild). React.lazy keeps the rejected import and the
    // browser caches the failed module fetch, so an in-place retry cannot
    // recover; offer a reload in both variants and keep the raw message out
    // of the visible text.
    if (isChunkLoadError(error)) {
      return (
        <div className={container}>
          <div className="text-center p-8 max-w-md">
            <div className="p-4 rounded-xl bg-status-pending/10 border border-status-pending/20 inline-block mb-4">
              <AlertCircle size={32} className="text-status-pending" />
            </div>
            <h1 className="text-lg text-text-primary mb-2">Reload needed</h1>
            <p className="text-sm text-text-secondary mb-4" title={error.message}>
              The web UI changed since this tab was loaded. Reload the page to continue.
            </p>
            <button
              onClick={() => boundaryReload.current()}
              className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary-light transition-colors"
            >
              Reload
            </button>
          </div>
        </div>
      );
    }

    return (
      <div className={container}>
        <div className="text-center p-8 max-w-md">
          <div className="p-4 rounded-xl bg-status-error/10 border border-status-error/20 inline-block mb-4">
            <AlertCircle size={32} className="text-status-error" />
          </div>
          <h1 className="text-lg text-status-error mb-2">Something went wrong</h1>
          <pre className="text-xs text-text-muted bg-surface p-4 rounded-lg overflow-auto max-h-32 mb-4">
            {error.message}
          </pre>
          <button
            onClick={inline ? this.handleRetry : () => boundaryReload.current()}
            className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary-light transition-colors"
          >
            {inline ? 'Try again' : 'Reload Window'}
          </button>
        </div>
      </div>
    );
  }
}
