import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, act } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import { useStackStore } from '../stores/useStackStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useVaultStore } from '../stores/useVaultStore';
import type { MCPServerStatus, ResourceStatus } from '../types';
import type { PackListItem, Variable } from '../lib/api';

function tile(label: string): HTMLElement {
  return screen.getByRole('button', { name: new RegExp(`${label} `) });
}

function badge(label: string): Element | null {
  return tile(label).querySelector('.rounded-full');
}

beforeEach(() => {
  cleanup();
  useWizardStore.getState().reset();
  useWizardStore.setState({ isOpen: true, currentStep: 'type' });
  useStackStore.setState({ stackName: 'demo', mcpServers: [], resources: [], clients: [] });
  useRegistryStore.setState({ skills: null, packs: null });
  useVaultStore.setState({ variables: null });
});

afterEach(() => {
  useWizardStore.getState().reset();
  useStackStore.setState({ stackName: '', mcpServers: [], resources: [], clients: [] });
  useRegistryStore.setState({ skills: null, packs: null });
  useVaultStore.setState({ variables: null });
});

describe('CreationWizard resource counts', () => {
  it('badges a positive known count and skips null or zero counts', () => {
    useStackStore.setState({
      stackName: 'demo',
      mcpServers: [{ name: 'one' }, { name: 'two' }] as MCPServerStatus[],
      resources: [] as ResourceStatus[],
      clients: [
        { name: 'Claude', slug: 'claude', detected: true, linked: true, transport: 'http' },
        { name: 'Cursor', slug: 'cursor', detected: true, linked: false, transport: 'http' },
      ],
    });

    render(<CreationWizard />);

    expect(badge('Stack')).toBeNull();
    expect(badge('Global Context')).toBeNull();
    expect(badge('Pack')).toBeNull();
    expect(badge('Variable')).toBeNull();
    expect(badge('Skill')).toBeNull();
    expect(badge('Resource')).toBeNull();
    expect(badge('MCP Server')).toHaveTextContent('2');
    expect(badge('Client Link')).toHaveTextContent('1');

    act(() => {
      useRegistryStore.setState({
        packs: [{ name: 'pack-a' }, { name: 'pack-b' }] as PackListItem[],
      });
      useVaultStore.setState({
        variables: [{ key: 'A' }, { key: 'B' }, { key: 'C' }] as Variable[],
      });
    });

    expect(badge('Pack')).toHaveTextContent('2');
    expect(badge('Variable')).toHaveTextContent('3');
    expect(badge('Stack')).toBeNull();
    expect(badge('Global Context')).toBeNull();
  });
});
