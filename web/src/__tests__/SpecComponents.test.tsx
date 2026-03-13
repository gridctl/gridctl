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
      specLoading: false,
      specError: null,
      validation: null,
      health: null,
      plan: null,
      compareActive: false,
      diffModalOpen: false,
      pendingSpec: null,
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
  });

  it('closes diff modal and clears pending spec', () => {
    useSpecStore.getState().openDiffModal('name: updated');
    useSpecStore.getState().closeDiffModal();
    expect(useSpecStore.getState().diffModalOpen).toBe(false);
    expect(useSpecStore.getState().pendingSpec).toBeNull();
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
      diffModalOpen: false,
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
});
