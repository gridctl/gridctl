import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useSpecStore } from '../stores/useSpecStore';
import { useUIStore } from '../stores/useUIStore';

// Mock API
vi.mock('../lib/api', () => ({
  fetchStackHealth: vi.fn().mockResolvedValue({
    validation: { status: 'valid', errorCount: 0, warningCount: 0 },
    drift: { status: 'in-sync' },
    dependencies: { status: 'resolved' },
  }),
  fetchStackSpec: vi.fn().mockResolvedValue({ path: '/tmp/stack.yaml', content: 'name: test' }),
  fetchStackPlan: vi.fn().mockResolvedValue({ hasChanges: false, items: [], summary: '' }),
  validateStackSpec: vi.fn().mockResolvedValue({ valid: true, errorCount: 0, warningCount: 0, issues: [] }),
  triggerReload: vi.fn().mockResolvedValue({ success: true, message: 'ok' }),
  fetchSecretsMap: vi.fn().mockResolvedValue({
    secrets: { 'API_KEY': ['server-a', 'server-b'] },
    nodes: { 'server-a': ['API_KEY'], 'server-b': ['API_KEY'] },
  }),
  fetchStackRecipes: vi.fn().mockResolvedValue([
    { id: 'test', name: 'Test Recipe', description: 'A test recipe', category: 'test', spec: 'name: test' },
  ]),
}));

import { SpecModeOverlay } from '../components/spec/SpecModeOverlay';
import { SecretHeatmapOverlay } from '../components/spec/SecretHeatmapOverlay';

// --- SpecModeOverlay tests ---

describe('SpecModeOverlay', () => {
  beforeEach(() => {
    useSpecStore.setState({
      health: null,
      plan: null,
    });
  });

  it('shows in-sync message when no drift', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: { status: 'in-sync' },
        dependencies: { status: 'resolved' },
      },
    });
    render(<SpecModeOverlay />);
    expect(screen.getByText(/all in sync/)).toBeInTheDocument();
  });

  it('shows ghost items for undeployed spec items', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: {
          status: 'drifted',
          added: ['ghost-server'],
          removed: [],
          changed: [],
        },
        dependencies: { status: 'resolved' },
      },
    });
    render(<SpecModeOverlay />);
    expect(screen.getByText('ghost-server')).toBeInTheDocument();
    expect(screen.getByText('Declared')).toBeInTheDocument();
  });

  it('shows warning items for untracked running items', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: {
          status: 'drifted',
          added: [],
          removed: ['untracked-server'],
          changed: [],
        },
        dependencies: { status: 'resolved' },
      },
    });
    render(<SpecModeOverlay />);
    expect(screen.getByText('untracked-server')).toBeInTheDocument();
    expect(screen.getByText('Untracked')).toBeInTheDocument();
  });
});

// --- SecretHeatmapOverlay tests ---

describe('SecretHeatmapOverlay', () => {
  it('renders loading state initially', () => {
    render(<SecretHeatmapOverlay />);
    expect(screen.getByText(/Loading secrets map/)).toBeInTheDocument();
  });
});

// --- useUIStore canvas mode toggle tests ---

describe('useUIStore canvas mode toggles', () => {
  beforeEach(() => {
    useUIStore.setState({
      showSpecMode: false,
      showWiringMode: false,
      showSecretHeatmap: false,
    });
  });

  it('toggles spec mode', () => {
    expect(useUIStore.getState().showSpecMode).toBe(false);
    useUIStore.getState().toggleSpecMode();
    expect(useUIStore.getState().showSpecMode).toBe(true);
    useUIStore.getState().toggleSpecMode();
    expect(useUIStore.getState().showSpecMode).toBe(false);
  });

  it('toggles wiring mode', () => {
    expect(useUIStore.getState().showWiringMode).toBe(false);
    useUIStore.getState().toggleWiringMode();
    expect(useUIStore.getState().showWiringMode).toBe(true);
    useUIStore.getState().toggleWiringMode();
    expect(useUIStore.getState().showWiringMode).toBe(false);
  });

  it('toggles secret heatmap', () => {
    expect(useUIStore.getState().showSecretHeatmap).toBe(false);
    useUIStore.getState().toggleSecretHeatmap();
    expect(useUIStore.getState().showSecretHeatmap).toBe(true);
    useUIStore.getState().toggleSecretHeatmap();
    expect(useUIStore.getState().showSecretHeatmap).toBe(false);
  });
});
