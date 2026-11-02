import { describe, expect, it, vi, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { ToolsWorkspace } from '../components/workspaces/ToolsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({ openDetachedWindow: vi.fn(), focusDetachedWindow: vi.fn() }),
}));

vi.mock('../hooks/useToolUsage', () => ({
  useToolUsage: () => ({ usage: null, loading: false, error: null }),
}));

vi.mock('../hooks/useLimits', () => ({
  useLimits: () => ({ limits: null, loading: false, error: null }),
}));

vi.mock('../hooks/useGroups', () => ({
  useGroups: () => ({ report: null, error: null }),
}));

vi.mock('../hooks/usePolicy', () => ({
  usePolicy: () => ({ report: null, error: null }),
}));

vi.mock('../hooks/useOptimize', () => ({
  useOptimize: () => ({ report: null, error: null }),
}));

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

function renderTools() {
  return render(
    <CommandRegistryProvider>
      <MemoryRouter>
        <ToolsWorkspace />
      </MemoryRouter>
    </CommandRegistryProvider>,
  );
}

describe('Fleet button', () => {
  beforeEach(() => {
    useUIStore.setState({ compactMode: { ...COMPACT_MODE_DEFAULTS } });
    useStackStore.setState({
      isLoading: false,
      error: null,
      mcpServers: [],
      tools: [],
      toolCatalog: [],
    });
  });

  it('stays focusable and explains an empty stack without opening the panel', () => {
    renderTools();
    const fleet = screen.getByRole('button', { name: 'Open fleet tool actions' });
    expect(fleet).not.toHaveAttribute('disabled');
    expect(fleet).toHaveAttribute('aria-disabled', 'true');
    expect(fleet).toHaveAttribute('title', 'No servers in the stack');
    fireEvent.click(fleet);
    expect(screen.queryByRole('dialog', { name: /fleet/i })).not.toBeInTheDocument();
  });

  it('opens the fleet panel when servers are present', () => {
    useStackStore.setState({
      mcpServers: [{ name: 'github', status: 'running', tools: [], toolWhitelist: [] } as never],
    });
    renderTools();
    const fleet = screen.getByRole('button', { name: 'Open fleet tool actions' });
    expect(fleet).toHaveAttribute('aria-disabled', 'false');
    expect(fleet).not.toHaveAttribute('title');
    fireEvent.click(fleet);
    expect(screen.getByRole('dialog', { name: /fleet/i })).toBeInTheDocument();
  });
});
