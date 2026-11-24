import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { fireEvent, render, screen } from '@testing-library/react';
import type { GatewayStatus, MCPServerStatus } from '../types';

const { showToast } = vi.hoisted(() => ({ showToast: vi.fn() }));
vi.mock('../components/ui/Toast', () => ({ showToast }));

import { AuthPendingBadge } from '../components/sidebar/AuthPendingBadge';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import {
  requestInspectorLeave,
  useInspectorToolsGuardStore,
} from '../stores/useInspectorToolsGuardStore';

function server(name: string, authStatus?: MCPServerStatus['authStatus']): MCPServerStatus {
  return { name, transport: 'http', initialized: true, toolCount: 0, authStatus } as MCPServerStatus;
}

function status(servers: MCPServerStatus[]): GatewayStatus {
  return { gateway: { name: 'gw', version: '0' }, 'mcp-servers': servers };
}

beforeEach(() => {
  showToast.mockReset();
  useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
  useStackStore.setState({ selectedNodeId: 'mcp-files', mcpServers: [] });
  useUIStore.setState({ sidebarOpen: true });
});

describe('requestInspectorLeave', () => {
  it('passes through when nothing is dirty and stashes otherwise', () => {
    const now = vi.fn();
    requestInspectorLeave(now);
    expect(now).toHaveBeenCalledTimes(1);

    useInspectorToolsGuardStore.setState({ dirtyServer: 'files' });
    const held = vi.fn();
    requestInspectorLeave(held, { targetServer: 'other' });
    expect(held).not.toHaveBeenCalled();
    expect(useInspectorToolsGuardStore.getState().pendingLeave).toBe(held);

    useInspectorToolsGuardStore.getState().cancelLeave();
    expect(useInspectorToolsGuardStore.getState().pendingLeave).toBeNull();
    expect(held).not.toHaveBeenCalled();
  });

  it('clears the dirty flag before running a confirmed leave', () => {
    useInspectorToolsGuardStore.setState({ dirtyServer: 'files' });
    const nested = vi.fn();
    requestInspectorLeave(() => requestInspectorLeave(nested));
    useInspectorToolsGuardStore.getState().confirmLeave();
    expect(nested).toHaveBeenCalledTimes(1);
    expect(useInspectorToolsGuardStore.getState().pendingLeave).toBeNull();
  });
});

describe('authorization entry points', () => {
  it('the status bar badge asks before leaving a dirty draft', () => {
    useStackStore.setState({ mcpServers: [server('vault', 'needs_auth')] });
    useInspectorToolsGuardStore.setState({ dirtyServer: 'files' });
    render(<AuthPendingBadge />);
    fireEvent.click(screen.getByRole('button', { name: /Authorization: 1 pending/ }));
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-files');
    useInspectorToolsGuardStore.getState().confirmLeave();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-vault');
  });

  it('the Authorize toast action asks before leaving a dirty draft', () => {
    useStackStore.getState().setGatewayStatus(status([server('vault')]));
    useStackStore.getState().setGatewayStatus(status([server('vault', 'needs_auth')]));
    const action = showToast.mock.calls.at(-1)?.[2]?.action as { onClick: () => void };
    useStackStore.setState({ selectedNodeId: 'mcp-files' });
    useInspectorToolsGuardStore.setState({ dirtyServer: 'files' });

    action.onClick();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-files');
    expect(useInspectorToolsGuardStore.getState().pendingLeave).not.toBeNull();
    useInspectorToolsGuardStore.getState().confirmLeave();
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-vault');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });
});
