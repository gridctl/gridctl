import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useSpecStore } from '../stores/useSpecStore';
import type { SpecHealth, ValidationResult, PlanDiff, StackSpec } from '../types';

// --- useSpecStore tests ---

describe('useSpecStore', () => {
  beforeEach(() => {
    useSpecStore.setState({
      spec: null,
      specLoading: false,
      specError: null,
      validation: null,
      health: null,
      plan: null,
      planLoading: false,
      planError: null,
      compareActive: false,
      diffModalOpen: false,
      diffModalMode: 'apply',
    });
  });

  it('sets spec content', () => {
    const spec: StackSpec = { path: '/tmp/stack.yaml', content: 'name: test' };
    useSpecStore.getState().setSpec(spec);
    expect(useSpecStore.getState().spec).toEqual(spec);
    expect(useSpecStore.getState().specError).toBeNull();
  });

  it('sets loading state', () => {
    useSpecStore.getState().setSpecLoading(true);
    expect(useSpecStore.getState().specLoading).toBe(true);
  });

  it('sets error', () => {
    useSpecStore.getState().setSpecError('Failed to load');
    expect(useSpecStore.getState().specError).toBe('Failed to load');
  });

  it('clears error when spec is set', () => {
    useSpecStore.getState().setSpecError('some error');
    useSpecStore.getState().setSpec({ path: '/tmp/stack.yaml', content: 'test' });
    expect(useSpecStore.getState().specError).toBeNull();
  });

  it('sets validation result', () => {
    const validation: ValidationResult = {
      valid: true,
      errorCount: 0,
      warningCount: 0,
      issues: [],
    };
    useSpecStore.getState().setValidation(validation);
    expect(useSpecStore.getState().validation).toEqual(validation);
  });

  it('sets health', () => {
    const health: SpecHealth = {
      validation: { status: 'valid', errorCount: 0, warningCount: 0 },
      drift: { status: 'in-sync' },
      dependencies: { status: 'resolved' },
    };
    useSpecStore.getState().setHealth(health);
    expect(useSpecStore.getState().health).toEqual(health);
  });

  it('sets plan diff and clears loading and error', () => {
    const plan: PlanDiff = {
      hasChanges: true,
      items: [{ action: 'add', kind: 'mcp-server', name: 'new-server' }],
      summary: '1 addition',
    };
    useSpecStore.setState({ planLoading: true, planError: new Error('x') });
    useSpecStore.getState().setPlan(plan);
    expect(useSpecStore.getState().plan).toEqual(plan);
    expect(useSpecStore.getState().planLoading).toBe(false);
    expect(useSpecStore.getState().planError).toBeNull();
  });

  it('toggles compare mode', () => {
    expect(useSpecStore.getState().compareActive).toBe(false);
    useSpecStore.getState().toggleCompare();
    expect(useSpecStore.getState().compareActive).toBe(true);
    useSpecStore.getState().toggleCompare();
    expect(useSpecStore.getState().compareActive).toBe(false);
  });

  it('opens the apply modal with a plan', () => {
    const plan: PlanDiff = { hasChanges: true, items: [{ action: 'remove', kind: 'resource', name: 'db' }], summary: '' };
    useSpecStore.getState().openApplyPlan(plan);
    const s = useSpecStore.getState();
    expect(s.diffModalOpen).toBe(true);
    expect(s.diffModalMode).toBe('apply');
    expect(s.plan).toEqual(plan);
    expect(s.planError).toBeNull();
  });

  it('opens the apply modal with a pre-flight error', () => {
    const err = new Error('boom');
    useSpecStore.getState().openApplyError(err);
    const s = useSpecStore.getState();
    expect(s.diffModalOpen).toBe(true);
    expect(s.diffModalMode).toBe('apply');
    expect(s.plan).toBeNull();
    expect(s.planError).toBe(err);
  });

  it('opens compare modal in compare mode in a loading state', () => {
    useSpecStore.getState().openCompareModal();
    const s = useSpecStore.getState();
    expect(s.diffModalOpen).toBe(true);
    expect(s.diffModalMode).toBe('compare');
    expect(s.planLoading).toBe(true);
  });

  it('closes the modal and clears plan, loading, and error', () => {
    useSpecStore.getState().openApplyError(new Error('boom'));
    useSpecStore.setState({ planLoading: true });
    useSpecStore.getState().closeDiffModal();
    const s = useSpecStore.getState();
    expect(s.diffModalOpen).toBe(false);
    expect(s.diffModalMode).toBe('apply');
    expect(s.plan).toBeNull();
    expect(s.planLoading).toBe(false);
    expect(s.planError).toBeNull();
  });
});

// --- SpecHealthBadge tests ---

// Mock API before importing component
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  fetchStackHealth: vi.fn().mockResolvedValue({
    validation: { status: 'valid', errorCount: 0, warningCount: 0 },
    drift: { status: 'in-sync' },
    dependencies: { status: 'resolved' },
  }),
  fetchStackSpec: vi.fn().mockResolvedValue({
    path: '/tmp/stack.yaml',
    content: 'name: test',
  }),
  fetchStackPlan: vi.fn().mockResolvedValue({
    hasChanges: false,
    items: [],
    summary: 'No changes',
  }),
  validateStackSpec: vi.fn().mockResolvedValue({
    valid: true,
    errorCount: 0,
    warningCount: 0,
    issues: [],
  }),
  triggerReload: vi.fn().mockResolvedValue({ success: true, message: 'Reloaded' }),
}));

import { MemoryRouter } from 'react-router';
import { SpecHealthBadge } from '../components/spec/SpecHealthBadge';

// SpecHealthBadge calls useNavigate (it deep-links /stack?spec=1), so it
// needs a router context.
function renderBadge() {
  return render(
    <MemoryRouter>
      <SpecHealthBadge />
    </MemoryRouter>,
  );
}

describe('SpecHealthBadge', () => {
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
      diffModalMode: 'apply',
    });
  });

  it('renders nothing when health is null', () => {
    const { container } = renderBadge();
    expect(container.firstChild).toBeNull();
  });

  it('renders "Spec: Valid" when spec is valid', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'valid', errorCount: 0, warningCount: 0 },
        drift: { status: 'in-sync' },
        dependencies: { status: 'resolved' },
      },
    });
    renderBadge();
    expect(screen.getByText('Spec: Valid')).toBeInTheDocument();
  });

  it('renders warning count when spec has warnings', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'warnings', errorCount: 0, warningCount: 3 },
        drift: { status: 'in-sync' },
        dependencies: { status: 'resolved' },
      },
    });
    renderBadge();
    expect(screen.getByText('Spec: 3 warnings')).toBeInTheDocument();
  });

  it('renders error count when spec has errors', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'errors', errorCount: 2, warningCount: 0 },
        drift: { status: 'in-sync' },
        dependencies: { status: 'resolved' },
      },
    });
    renderBadge();
    expect(screen.getByText('Spec: 2 errors')).toBeInTheDocument();
  });

  it('renders singular warning text', () => {
    useSpecStore.setState({
      health: {
        validation: { status: 'warnings', errorCount: 0, warningCount: 1 },
        drift: { status: 'in-sync' },
        dependencies: { status: 'resolved' },
      },
    });
    renderBadge();
    expect(screen.getByText('Spec: 1 warning')).toBeInTheDocument();
  });
});

// --- SpecDiffModal tests ---

import { SpecDiffModal } from '../components/spec/SpecDiffModal';
import { fetchStackPlan, fetchStackHealth } from '../lib/api';

const RESET_MODAL = {
  spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
  diffModalOpen: false,
  diffModalMode: 'apply' as const,
  health: null,
  specLoading: false,
  specError: null,
  validation: null,
  plan: null,
  planLoading: false,
  planError: null,
  compareActive: false,
};

const CHANGE_PLAN: PlanDiff = {
  hasChanges: true,
  items: [
    { action: 'change', kind: 'mcp-server', name: 'github', details: ['image changed'] },
    { action: 'add', kind: 'resource', name: 'postgres' },
    { action: 'remove', kind: 'mcp-server', name: 'old', override: true },
  ],
  summary: '1 to add, 1 to change, 1 to remove',
  override: '/tmp/stack.override.yaml',
};

describe('SpecDiffModal', () => {
  beforeEach(() => {
    vi.mocked(fetchStackPlan).mockReset();
    vi.mocked(fetchStackHealth).mockReset();
    vi.mocked(fetchStackHealth).mockResolvedValue({
      validation: { status: 'valid', errorCount: 0, warningCount: 0 },
      drift: { status: 'drifted', changed: ['github'] },
      dependencies: { status: 'resolved' },
    });
    useSpecStore.setState(RESET_MODAL);
  });

  it('does not render when modal is closed', () => {
    const { container } = render(<SpecDiffModal onApply={vi.fn()} />);
    expect(container.querySelector('.fixed')).toBeNull();
  });

  it('renders an apply-mode plan as a structured list', () => {
    useSpecStore.getState().openApplyPlan(CHANGE_PLAN);
    render(<SpecDiffModal onApply={vi.fn()} />);
    expect(screen.getByRole('dialog', { name: 'Reload Config' })).toBeInTheDocument();
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('image changed')).toBeInTheDocument();
    expect(screen.getByText('postgres')).toBeInTheDocument();
    expect(screen.getByText('from override file')).toBeInTheDocument();
    expect(screen.getByText('/tmp/stack.override.yaml')).toBeInTheDocument();
    expect(screen.getByText('1 added')).toBeInTheDocument();
    expect(screen.getByText('1 changed')).toBeInTheDocument();
    expect(screen.getByText('1 removed')).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'MCP servers' })).toBeInTheDocument();
    expect(screen.getByRole('region', { name: 'Resources' })).toBeInTheDocument();
    expect(document.querySelector('table')).toBeNull();
  });

  it('shows the limits note naming ${secret:} in apply mode', () => {
    useSpecStore.getState().openApplyPlan(CHANGE_PLAN);
    render(<SpecDiffModal onApply={vi.fn()} />);
    const note = screen.getByText(/does not resolve \$\{secret:\} references/);
    expect(note).toHaveTextContent('startup security check');
    expect(note).toHaveTextContent('clients, limits, policy, groups, skills, model_preferences, runs, and experimental');
  });

  it('calls onApply once and closes when the confirm button is clicked', () => {
    useSpecStore.getState().openApplyPlan(CHANGE_PLAN);
    const onApply = vi.fn();
    const onDismiss = vi.fn();
    render(<SpecDiffModal onApply={onApply} onDismiss={onDismiss} />);
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }));
    expect(onApply).toHaveBeenCalledTimes(1);
    expect(onDismiss).not.toHaveBeenCalled();
    expect(useSpecStore.getState().diffModalOpen).toBe(false);
  });

  it('dismisses without applying on Cancel, Close, and Escape', () => {
    const onApply = vi.fn();
    const onDismiss = vi.fn();
    useSpecStore.getState().openApplyPlan(CHANGE_PLAN);
    const { rerender } = render(<SpecDiffModal onApply={onApply} onDismiss={onDismiss} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(useSpecStore.getState().diffModalOpen).toBe(false);

    act(() => useSpecStore.getState().openApplyPlan(CHANGE_PLAN));
    rerender(<SpecDiffModal onApply={onApply} onDismiss={onDismiss} />);
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));

    act(() => useSpecStore.getState().openApplyPlan(CHANGE_PLAN));
    rerender(<SpecDiffModal onApply={onApply} onDismiss={onDismiss} />);
    fireEvent.keyDown(document, { key: 'Escape' });

    expect(onDismiss).toHaveBeenCalledTimes(3);
    expect(onApply).not.toHaveBeenCalled();
    expect(useSpecStore.getState().diffModalOpen).toBe(false);
  });

  it('renders a pre-flight error with Cancel and Reload without preview', () => {
    useSpecStore.getState().openApplyError(new Error('candidate invalid'));
    const onApply = vi.fn();
    const onDismiss = vi.fn();
    render(<SpecDiffModal onApply={onApply} onDismiss={onDismiss} />);
    expect(screen.getByRole('alert')).toHaveTextContent('Reload preview failed: candidate invalid');
    expect(screen.queryByRole('button', { name: 'Reload' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onApply).not.toHaveBeenCalled();
    expect(onDismiss).toHaveBeenCalledTimes(1);

    act(() => useSpecStore.getState().openApplyError(new Error('candidate invalid')));
    fireEvent.click(screen.getByRole('button', { name: 'Reload without preview' }));
    expect(onApply).toHaveBeenCalledTimes(1);
  });

  it('compare mode renders the server plan regardless of the spec in the store', async () => {
    vi.mocked(fetchStackPlan).mockResolvedValue(CHANGE_PLAN);
    // Hard-refresh case: the store spec already equals the edited file.
    useSpecStore.setState({ spec: { path: '/tmp/stack.yaml', content: 'edited' } });
    useSpecStore.getState().openCompareModal();
    render(<SpecDiffModal onApply={vi.fn()} />);
    expect(screen.getByRole('dialog', { name: 'Compare to running' })).toBeInTheDocument();
    expect(screen.getByRole('status')).toHaveTextContent(/Comparing/);
    expect(await screen.findByText('github')).toBeInTheDocument();
    expect(fetchStackPlan).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('button', { name: 'Reload' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: 'Close' })).toHaveLength(2);
    await waitFor(() => expect(useSpecStore.getState().health?.drift.status).toBe('drifted'));
  });

  it('compare mode with an empty plan renders the qualified message and limits note', async () => {
    vi.mocked(fetchStackPlan).mockResolvedValue({ hasChanges: false, items: [], summary: 'No changes detected.' });
    useSpecStore.getState().openCompareModal();
    render(<SpecDiffModal onApply={vi.fn()} />);
    expect(
      await screen.findByText(/Servers, resources, gateway settings, and networks in the stack file match the running gateway/),
    ).toBeInTheDocument();
    expect(screen.getByText(/does not resolve \$\{secret:\} references/)).toBeInTheDocument();
    expect(screen.queryByText(/No drift/)).not.toBeInTheDocument();
  });

  it('compare mode shows the error and retries on Retry', async () => {
    vi.mocked(fetchStackPlan)
      .mockRejectedValueOnce(new Error('Failed to load stack file: bad yaml'))
      .mockResolvedValueOnce(CHANGE_PLAN);
    useSpecStore.getState().openCompareModal();
    render(<SpecDiffModal onApply={vi.fn()} />);
    expect(await screen.findByRole('alert')).toHaveTextContent('Compare failed: Failed to load stack file: bad yaml');
    expect(screen.queryByText(/does not resolve/)).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('github')).toBeInTheDocument();
    expect(fetchStackPlan).toHaveBeenCalledTimes(2);
  });

  it('ignores a compare response that arrives after the modal closed', async () => {
    let resolve!: (plan: PlanDiff) => void;
    vi.mocked(fetchStackPlan).mockReturnValue(new Promise((r) => { resolve = r; }));
    useSpecStore.getState().openCompareModal();
    render(<SpecDiffModal onApply={vi.fn()} />);
    fireEvent.click(screen.getAllByRole('button', { name: 'Close' })[1]);
    await act(async () => { resolve(CHANGE_PLAN); });
    expect(useSpecStore.getState().plan).toBeNull();
    expect(fetchStackHealth).not.toHaveBeenCalled();
    expect(useSpecStore.getState().health).toBeNull();
  });

  it('ignores a compare response superseded by a newer request', async () => {
    let resolveFirst!: (plan: PlanDiff) => void;
    vi.mocked(fetchStackPlan)
      .mockRejectedValueOnce(new Error('first failed'))
      .mockReturnValueOnce(new Promise((r) => { resolveFirst = r; }))
      .mockResolvedValueOnce({ hasChanges: false, items: [], summary: '' });
    useSpecStore.getState().openCompareModal();
    render(<SpecDiffModal onApply={vi.fn()} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Retry' }));
    // Close and reopen while the second request is pending.
    fireEvent.click(screen.getAllByRole('button', { name: 'Close' })[0]);
    act(() => useSpecStore.getState().openCompareModal());
    expect(await screen.findByText(/match the running gateway/)).toBeInTheDocument();
    await act(async () => { resolveFirst(CHANGE_PLAN); });
    expect(screen.queryByText('github')).not.toBeInTheDocument();
  });
});

// --- SpecTab tests ---

import { SpecTab } from '../components/spec/SpecTab';

describe('SpecTab', () => {
  beforeEach(() => {
    useSpecStore.setState({
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
      diffModalOpen: false,
      diffModalMode: 'apply',
      health: null,
      specLoading: false,
      specError: null,
      validation: { valid: true, errorCount: 0, warningCount: 0, issues: [] },
      plan: null,
      compareActive: false,
    });
  });

  it('opens compare modal when "Compare to running" is clicked', () => {
    render(<SpecTab />);
    fireEvent.click(screen.getByText('Compare to running'));
    expect(useSpecStore.getState().diffModalOpen).toBe(true);
    expect(useSpecStore.getState().diffModalMode).toBe('compare');
  });
});

// --- SpecPane tests ---

import { SpecPane } from '../components/spec/SpecPane';

describe('SpecPane', () => {
  beforeEach(() => {
    useSpecStore.setState({
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
      diffModalOpen: false,
      diffModalMode: 'apply',
      health: null,
      specLoading: false,
      specError: null,
      validation: { valid: true, errorCount: 0, warningCount: 0, issues: [] },
      plan: null,
      compareActive: false,
    });
  });

  it('renders the spec content inside the pane', () => {
    render(<SpecPane onClose={vi.fn()} />);
    expect(screen.getByRole('dialog', { name: 'Spec' })).toBeInTheDocument();
    expect(screen.getByText('/tmp/stack.yaml')).toBeInTheDocument();
    expect(screen.getByText('Compare to running')).toBeInTheDocument();
  });

  it('invokes onClose from the close button', () => {
    const onClose = vi.fn();
    render(<SpecPane onClose={onClose} />);
    fireEvent.click(screen.getByRole('button', { name: 'Close spec pane' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes on Escape', () => {
    const onClose = vi.fn();
    render(<SpecPane onClose={onClose} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
