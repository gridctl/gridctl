import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

import { ServerCrashDetails } from '../components/sidebar/ServerCrashDetails';
import type { MCPServerNodeData } from '../types';

function data(overrides: Partial<MCPServerNodeData> = {}): MCPServerNodeData {
  return {
    type: 'mcp-server',
    name: 'crash',
    transport: 'stdio',
    initialized: true,
    toolCount: 0,
    tools: [],
    status: 'restarting',
    healthy: false,
    ...overrides,
  };
}

describe('ServerCrashDetails', () => {
  it('renders exit fields and recent stderr beside the caller-supplied panel', () => {
    render(<ServerCrashDetails data={data({
      replicas: [{
        replicaId: 0,
        state: 'restarting',
        healthy: false,
        inFlight: 0,
        restartAttempts: 1,
        nextRetryAt: new Date(Date.now() + 4000).toISOString(),
        exit: { code: 3, oomKilled: false, status: 'exited', finishedAt: new Date().toISOString() },
      }],
      stderrTail: ['fatal: refusing to continue'],
    })} />);

    expect(screen.getByText('restarting')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
    expect(screen.getByText('exited')).toBeInTheDocument();
    expect(screen.getByText('Recent stderr')).toBeInTheDocument();
    expect(screen.getByText('fatal: refusing to continue')).toBeInTheDocument();
  });
});
