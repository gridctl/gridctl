import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useSpecStore } from '../stores/useSpecStore';
import type { SpecHealth, ValidationResult, PlanDiff, StackSpec } from '../types';

// --- useSpecStore tests ---

describe('useSpecStore', () => {
  beforeEach(() => {
    useSpecStore.setState({
      spec: null,
      appliedSpec: null,
      specLoading: false,
      specError: null,
      validation: null,
      health: null,
      plan: null,
      compareActive: false,
      diffModalOpen: false,
      diffModalMode: 'apply',
      pendingSpec: null,
    });
  });

  it('sets spec content', () => {
    const spec: StackSpec = { path: '/tmp/stack.yaml', content: 'name: test' };
    useSpecStore.getState().setSpec(spec);
    expect(useSpecStore.getState().spec).toEqual(spec);
    expect(useSpecStore.getState().specError).toBeNull();
  });

  it('sets appliedSpec on first setSpec call', () => {
    const spec: StackSpec = { path: '/tmp/stack.yaml', content: 'name: test' };
    useSpecStore.getState().setSpec(spec);
    expect(useSpecStore.getState().appliedSpec).toEqual(spec);
  });

  it('does not overwrite appliedSpec on subsequent setSpec calls', () => {
    const first: StackSpec = { path: '/tmp/stack.yaml', content: 'name: first' };
    const second: StackSpec = { path: '/tmp/stack.yaml', content: 'name: second' };
    useSpecStore.getState().setSpec(first);
    useSpecStore.getState().setSpec(second);
    expect(useSpecStore.getState().spec).toEqual(second);
    expect(useSpecStore.getState().appliedSpec).toEqual(first);
  });

  it('updates appliedSpec via setAppliedSpec', () => {
    const first: StackSpec = { path: '/tmp/stack.yaml', content: 'name: first' };
    const second: StackSpec = { path: '/tmp/stack.yaml', content: 'name: second' };
    useSpecStore.getState().setSpec(first);
    useSpecStore.getState().setAppliedSpec(second);
    expect(useSpecStore.getState().appliedSpec).toEqual(second);
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

  it('sets plan diff', () => {
    const plan: PlanDiff = {
      hasChanges: true,
      items: [{ action: 'add', kind: 'mcp-server', name: 'new-server' }],
      summary: '1 addition',
    };
    useSpecStore.getState().setPlan(plan);
    expect(useSpecStore.getState().plan).toEqual(plan);
  });

  it('toggles compare mode', () => {
    expect(useSpecStore.getState().compareActive).toBe(false);
    useSpecStore.getState().toggleCompare();
    expect(useSpecStore.getState().compareActive).toBe(true);
    useSpecStore.getState().toggleCompare();
    expect(useSpecStore.getState().compareActive).toBe(false);
  });

  it('opens diff modal with pending spec', () => {
    useSpecStore.getState().openDiffModal('name: updated');
    expect(useSpecStore.getState().diffModalOpen).toBe(true);
    expect(useSpecStore.getState().pendingSpec).toBe('name: updated');
    expect(useSpecStore.getState().diffModalMode).toBe('apply');
  });

  it('closes diff modal and clears pending spec', () => {
    useSpecStore.getState().openDiffModal('name: updated');
    useSpecStore.getState().closeDiffModal();
    expect(useSpecStore.getState().diffModalOpen).toBe(false);
    expect(useSpecStore.getState().pendingSpec).toBeNull();
  });

  it('opens compare modal in compare mode without pending spec', () => {
    useSpecStore.getState().openCompareModal();
    expect(useSpecStore.getState().diffModalOpen).toBe(true);
    expect(useSpecStore.getState().diffModalMode).toBe('compare');
    expect(useSpecStore.getState().pendingSpec).toBeNull();
  });

  it('resets diffModalMode to apply when modal is closed', () => {
    useSpecStore.getState().openCompareModal();
    expect(useSpecStore.getState().diffModalMode).toBe('compare');
    useSpecStore.getState().closeDiffModal();
    expect(useSpecStore.getState().diffModalMode).toBe('apply');
  });
});

// --- SpecHealthBadge tests ---

// Mock API before importing component
vi.mock('../lib/api', () => ({
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

import { SpecHealthBadge } from '../components/spec/SpecHealthBadge';

describe('SpecHealthBadge', () => {
  beforeEach(() => {
    useSpecStore.setState({
      health: null,
      spec: null,
      appliedSpec: null,
      specLoading: false,
      specError: null,
      validation: null,
      plan: null,
      compareActive: false,
      diffModalOpen: false,
      diffModalMode: 'apply',
      pendingSpec: null,
    });
  });

  it('renders nothing when health is null', () => {
    const { container } = render(<SpecHealthBadge />);
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
    render(<SpecHealthBadge />);
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
    render(<SpecHealthBadge />);
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
    render(<SpecHealthBadge />);
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
    render(<SpecHealthBadge />);
    expect(screen.getByText('Spec: 1 warning')).toBeInTheDocument();
  });
});

// --- SpecDiffModal tests ---

import { SpecDiffModal } from '../components/spec/SpecDiffModal';

describe('SpecDiffModal', () => {
  beforeEach(() => {
    useSpecStore.setState({
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
      appliedSpec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
      diffModalOpen: false,
      diffModalMode: 'apply',
      pendingSpec: null,
      health: null,
      specLoading: false,
      specError: null,
      validation: null,
      plan: null,
      compareActive: false,
    });
  });

  it('does not render when modal is closed', () => {
    const onApply = vi.fn();
    const { container } = render(<SpecDiffModal onApply={onApply} />);
    expect(container.querySelector('.fixed')).toBeNull();
  });

  it('renders diff when modal is open with changes', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      pendingSpec: 'name: test\nversion: "2"',
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    expect(screen.getByText('Configuration Changed')).toBeInTheDocument();
    expect(screen.getByText('Apply Changes')).toBeInTheDocument();
    expect(screen.getByText('Cancel')).toBeInTheDocument();
  });

  it('shows no changes when specs are identical', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      pendingSpec: 'name: test\nversion: "1"',
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    expect(screen.getByText('No changes detected')).toBeInTheDocument();
  });

  it('disables Apply when validation errors exist', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      pendingSpec: 'name: test\nversion: "2"',
    });
    const onApply = vi.fn();
    render(
      <SpecDiffModal onApply={onApply} validationErrors={['name: required field']} />
    );
    const applyBtn = screen.getByText('Apply Changes');
    expect(applyBtn).toBeDisabled();
  });

  it('calls onApply and closes modal when Apply is clicked', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      pendingSpec: 'name: test\nversion: "2"',
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    fireEvent.click(screen.getByText('Apply Changes'));
    expect(onApply).toHaveBeenCalled();
    expect(useSpecStore.getState().diffModalOpen).toBe(false);
  });

  it('closes modal without applying when Cancel is clicked', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      pendingSpec: 'name: updated',
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    fireEvent.click(screen.getByText('Cancel'));
    expect(onApply).not.toHaveBeenCalled();
    expect(useSpecStore.getState().diffModalOpen).toBe(false);
  });

  it('shows validation error messages', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      pendingSpec: 'name: test\nbad: field',
    });
    const onApply = vi.fn();
    render(
      <SpecDiffModal
        onApply={onApply}
        validationErrors={['servers: at least one server required', 'name: invalid format']}
      />
    );
    expect(screen.getByText('Validation errors in new spec')).toBeInTheDocument();
    expect(screen.getByText('servers: at least one server required')).toBeInTheDocument();
    expect(screen.getByText('name: invalid format')).toBeInTheDocument();
  });

  it('renders compare-mode title and hides Apply button', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      diffModalMode: 'compare',
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "2"' },
      appliedSpec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    expect(screen.getByText('Compare to Running')).toBeInTheDocument();
    expect(screen.queryByText('Apply Changes')).not.toBeInTheDocument();
    expect(screen.getByText('Close')).toBeInTheDocument();
  });

  it('renders no-drift empty state in compare mode when specs match', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      diffModalMode: 'compare',
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
      appliedSpec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    expect(
      screen.getByText('No drift — on-disk spec matches the running gateway.'),
    ).toBeInTheDocument();
    expect(screen.queryByText('Apply Changes')).not.toBeInTheDocument();
  });

  it('renders diff content in compare mode when specs differ', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      diffModalMode: 'compare',
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "2"' },
      appliedSpec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    // Modal renders via createPortal — query the document root
    expect(document.querySelector('.text-status-running')).not.toBeNull();
    expect(document.querySelector('.line-through')).not.toBeNull();
  });

  it('renders missing-baseline state when appliedSpec is null in compare mode', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      diffModalMode: 'compare',
      spec: { path: '/tmp/stack.yaml', content: 'name: test' },
      appliedSpec: null,
    });
    const onApply = vi.fn();
    render(<SpecDiffModal onApply={onApply} />);
    expect(
      screen.getByText('Waiting for the gateway baseline — reload once to capture it.'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('No drift — on-disk spec matches the running gateway.'),
    ).not.toBeInTheDocument();
  });

  it('hides validation errors block in compare mode', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      diffModalMode: 'compare',
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "2"' },
      appliedSpec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
    });
    const onApply = vi.fn();
    render(
      <SpecDiffModal
        onApply={onApply}
        validationErrors={['this should not appear']}
      />,
    );
    expect(screen.queryByText('Validation errors in new spec')).not.toBeInTheDocument();
    expect(screen.queryByText('this should not appear')).not.toBeInTheDocument();
  });
});

// --- SpecTab tests ---

import { SpecTab } from '../components/spec/SpecTab';

describe('SpecTab', () => {
  beforeEach(() => {
    useSpecStore.setState({
      spec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
      appliedSpec: { path: '/tmp/stack.yaml', content: 'name: test\nversion: "1"' },
      diffModalOpen: false,
      diffModalMode: 'apply',
      pendingSpec: null,
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
