import { describe, it, expect, vi, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { VaultWorkspace } from '../components/workspaces/VaultWorkspace';
import { LogsWorkspace } from '../components/workspaces/LogsWorkspace';
import { ToolsWorkspace } from '../components/workspaces/ToolsWorkspace';
import { PinsWorkspace } from '../components/workspaces/PinsWorkspace';
import { MetricsWorkspace } from '../components/workspaces/MetricsWorkspace';
import { useVaultStore } from '../stores/useVaultStore';
import { useStackStore } from '../stores/useStackStore';
import { usePinsStore } from '../stores/usePinsStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { WORKSPACE_LABELS } from '../types/workspace';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));

vi.mock('../hooks/useLogStream', () => ({
  useLogStream: () => ({
    logs: [],
    isLoading: false,
    error: null,
    bufferTotal: 0,
    bufferCapacity: 0,
    lastLoadedAt: 0,
    refresh: vi.fn(),
    clear: vi.fn(),
  }),
}));

vi.mock('../hooks/useMetricsSeries', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../hooks/useMetricsSeries')>();
  return {
    ...actual,
    useMetricsSeries: () => ({
      metricsData: null,
      isLoading: false,
      error: null,
      reload: vi.fn(),
      clear: vi.fn(),
    }),
  };
});

vi.mock('../hooks/useOptimize', () => ({
  useOptimize: () => ({ report: null, error: null }),
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

vi.mock('../hooks/useSkillUsage', () => ({
  useSkillUsage: () => ({ usage: null, fetchedAt: null, loading: false, error: null }),
}));

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchVariables: vi.fn().mockResolvedValue([]),
    fetchVariableSets: vi.fn().mockResolvedValue([]),
    fetchVariableUsage: vi.fn().mockResolvedValue({}),
    fetchVariableDrift: vi.fn().mockResolvedValue([]),
    fetchVariableStoreStatus: vi.fn().mockResolvedValue({ locked: false, encrypted: false }),
    fetchGroups: vi.fn().mockResolvedValue({ configured: false, groups: [] }),
    fetchPolicy: vi.fn().mockResolvedValue({ configured: false, rules: [], tools: {} }),
    fetchRegistrySkills: vi.fn().mockResolvedValue([]),
    fetchRegistryStatus: vi.fn().mockResolvedValue({ totalSkills: 0, activeSkills: 0 }),
    fetchSkillSources: vi.fn().mockResolvedValue([]),
    fetchPacks: vi.fn().mockResolvedValue([]),
    fetchServerPins: vi.fn().mockResolvedValue({}),
    fetchSkillPins: vi.fn().mockResolvedValue({}),
  };
});

function renderWorkspace(node: React.ReactNode) {
  return render(
    <CommandRegistryProvider>
      <MemoryRouter>{node}</MemoryRouter>
    </CommandRegistryProvider>,
  );
}

describe('workspace landmarks', () => {
  beforeEach(() => {
    useVaultStore.setState({
      variables: [],
      sets: [],
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
    useStackStore.setState({
      isLoading: false,
      error: null,
      mcpServers: [],
      tools: [],
      toolCatalog: [],
    });
    usePinsStore.setState({
      pins: {},
      pinsError: null,
      skillPins: {},
      skillPinsError: null,
    });
    useRegistryStore.setState({
      skills: [],
      sources: [],
      error: null,
    });
  });

  it('vault, logs, tools, pins, and metrics have one heading and no nested main', () => {
    const cases = [
      { name: WORKSPACE_LABELS.vault, node: <VaultWorkspace /> },
      { name: WORKSPACE_LABELS.logs, node: <LogsWorkspace /> },
      { name: WORKSPACE_LABELS.tools, node: <ToolsWorkspace /> },
      { name: WORKSPACE_LABELS.pins, node: <PinsWorkspace /> },
      { name: WORKSPACE_LABELS.metrics, node: <MetricsWorkspace /> },
    ];
    for (const testCase of cases) {
      const { unmount } = renderWorkspace(testCase.node);
      expect(screen.queryAllByRole('main')).toHaveLength(0);
      expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
      expect(screen.getByRole('heading', { level: 1, name: testCase.name })).toBeInTheDocument();
      unmount();
    }
  });

});
