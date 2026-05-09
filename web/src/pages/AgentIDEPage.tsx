import { Component, type ReactNode } from 'react';
import { AgentIDE } from '../components/agent/ide/AgentIDE';

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class IDEErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="h-screen w-screen bg-background flex items-center justify-center">
          <div className="text-center p-8 max-w-md">
            <div className="font-sans text-text-muted/40 text-[10px] uppercase tracking-[0.4em] mb-3">
              agent ide
            </div>
            <h1 className="font-sans text-2xl text-status-error mb-2">IDE crashed</h1>
            <pre className="text-xs text-text-muted bg-surface p-4 rounded max-w-lg overflow-auto font-mono">
              {this.state.error?.message ?? 'unknown error'}
            </pre>
            <button
              onClick={() => window.location.reload()}
              className="mt-4 px-4 py-2 bg-primary text-background rounded font-mono text-xs uppercase tracking-[0.2em]"
            >
              reload
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

export default function AgentIDEPage() {
  return (
    <IDEErrorBoundary>
      <AgentIDE />
    </IDEErrorBoundary>
  );
}
