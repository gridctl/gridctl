import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';

vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
}));

const patchServerEnabled = vi.fn();
const fetchStatus = vi.fn();

vi.mock('../lib/api', () => ({
  patchServerEnabled: (...args: unknown[]) => patchServerEnabled(...args),
  fetchStatus: (...args: unknown[]) => fetchStatus(...args),
  SetServerToolsError: class SetServerToolsError extends Error {
    code: string;
    hint?: string;
    constructor(code: string, message: string, hint?: string, _httpStatus?: number) {
      super(message);
      this.code = code;
      this.hint = hint;
    }
  },
  RestartRequiredError: class RestartRequiredError extends Error {
    code = 'restart_required';
  },
}));

import CustomNode from '../components/graph/CustomNode';
import { useUIStore } from '../stores/useUIStore';
import { ServerEnableControl } from '../components/sidebar/ServerEnableControl';
import { SetServerToolsError } from '../lib/api';
import type { MCPServerNodeData } from '../types';

function makeServerData(overrides: Partial<MCPServerNodeData> = {}): MCPServerNodeData {
  return {
    type: 'mcp-server',
    name: 'parked',
    transport: 'stdio',
    initialized: false,
    toolCount: 0,
    tools: [],
    status: 'stopped',
    ...overrides,
  };
}

describe('CustomNode disabled server', () => {
  afterEach(() => {
    useUIStore.setState({ compactCards: true });
  });

  it('renders a muted disabled chip and hides health strips', () => {
    // The disabled status lives in the card body, which compact mode
    // removes from the accessibility tree.
    useUIStore.setState({ compactCards: false });
    const { container } = render(
      <CustomNode
        data={makeServerData({
          enabled: false,
          healthy: false,
          healthError: 'down',
          authStatus: 'needs_auth',
        })}
      />,
    );
    expect(screen.getByRole('status', { name: 'parked is disabled' })).toBeInTheDocument();
    expect(screen.getByText('disabled')).toBeInTheDocument();
    expect(screen.queryByText('Healthy')).not.toBeInTheDocument();
    expect(screen.queryByText('down')).not.toBeInTheDocument();
    expect(screen.queryByText('Needs authorization')).not.toBeInTheDocument();
    expect(container.firstChild).toHaveClass('opacity-60');
  });

  it('still shows the healthy line for an enabled server', () => {
    render(<CustomNode data={makeServerData({ name: 'live', enabled: true, healthy: true, status: 'running' })} />);
    expect(screen.getByText('Healthy')).toBeInTheDocument();
    expect(screen.queryByText('disabled')).not.toBeInTheDocument();
  });
});

describe('ServerEnableControl', () => {
  beforeEach(() => {
    patchServerEnabled.mockReset();
    fetchStatus.mockReset();
  });

  it('offers Enable for a disabled server', () => {
    render(<ServerEnableControl name="parked" enabled={false} />);
    expect(screen.getByRole('button', { name: 'Enable' })).toBeInTheDocument();
  });

  it('offers Disable for an enabled server', () => {
    render(<ServerEnableControl name="live" enabled />);
    expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument();
  });

  it('directs override_required to the CLI without claiming success', async () => {
    patchServerEnabled.mockRejectedValue(
      new SetServerToolsError(
        'override_required',
        'This stack writes enable and disable to an override file.',
        'Use gridctl enable or gridctl disable.',
        409,
      ),
    );
    render(<ServerEnableControl name="parked" enabled={false} overrides={['enabled']} />);
    fireEvent.click(screen.getByRole('button', { name: 'Enable' }));
    await waitFor(() => {
      expect(screen.getByRole('status')).toHaveTextContent('gridctl enable');
    });
    expect(screen.getByText('overridden locally')).toBeInTheDocument();
    expect(fetchStatus).not.toHaveBeenCalled();
  });
});
