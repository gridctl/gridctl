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
}));

import { DriftOverlay } from '../components/spec/DriftOverlay';

describe('DriftOverlay', () => {
  beforeEach(() => {
    useSpecStore.setState({
      health: null,
      spec: null,
      specLoading: false,
      specError: null,
      validation: null,
      plan: null,
      compareActive: false,
      diffModalOpen: false,
      pendingSpec: null,
    });
  });

  it('renders nothing when health is null', () => {
    const { container } = render(<DriftOverlay />);
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing when drift is in-sync', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: { status: 'in-sync' },
        dependencies: { status: 'resolved' },
      },
    });
    const { container } = render(<DriftOverlay />);
    expect(container.firstChild).toBeNull();
  });

  it('renders ghost items for nodes in spec but not running', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: {
          status: 'drifted',
          added: ['new-server', 'new-agent'],
          removed: [],
          changed: [],
        },
        dependencies: { status: 'resolved' },
      },
    });
    render(<DriftOverlay />);
    expect(screen.getByText('new-server')).toBeInTheDocument();
    expect(screen.getByText('new-agent')).toBeInTheDocument();
    expect(screen.getByText('2 not deployed')).toBeInTheDocument();
  });

  it('renders warning items for nodes running but not in spec', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: {
          status: 'drifted',
          added: [],
          removed: ['orphan-server'],
          changed: [],
        },
        dependencies: { status: 'resolved' },
      },
    });
    render(<DriftOverlay />);
    expect(screen.getByText('orphan-server')).toBeInTheDocument();
    expect(screen.getByText('1 not in spec')).toBeInTheDocument();
  });

  it('renders combined summary for mixed drift', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: {
          status: 'drifted',
          added: ['new-server'],
          removed: ['orphan'],
          changed: ['modified-server'],
        },
        dependencies: { status: 'resolved' },
      },
    });
    render(<DriftOverlay />);
    expect(screen.getByText('1 not deployed, 1 not in spec, 1 changed')).toBeInTheDocument();
  });
});

// --- useUIStore drift toggle tests ---

describe('useUIStore drift toggle', () => {
  beforeEach(() => {
    useUIStore.setState({ showDriftOverlay: false });
  });

  it('defaults to false', () => {
    expect(useUIStore.getState().showDriftOverlay).toBe(false);
  });

  it('toggles drift overlay', () => {
    useUIStore.getState().toggleDriftOverlay();
    expect(useUIStore.getState().showDriftOverlay).toBe(true);
    useUIStore.getState().toggleDriftOverlay();
    expect(useUIStore.getState().showDriftOverlay).toBe(false);
  });
});
