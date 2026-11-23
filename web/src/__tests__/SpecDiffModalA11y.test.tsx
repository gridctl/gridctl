import { beforeEach, describe, expect, it } from 'vitest';
import '@testing-library/jest-dom';
import { cleanup, render, screen } from '@testing-library/react';
import { SpecDiffModal } from '../components/spec/SpecDiffModal';
import { useSpecStore } from '../stores/useSpecStore';

beforeEach(() => {
  cleanup();
  useSpecStore.setState({
    diffModalOpen: false,
    diffModalMode: 'apply',
    plan: null,
    planLoading: false,
    planError: null,
    spec: null,
  });
});

describe('SpecDiffModal dialog semantics', () => {
  it('exposes a labeled modal dialog and a named close button', () => {
    useSpecStore.getState().openApplyPlan({
      hasChanges: true,
      items: [{ action: 'change', kind: 'mcp-server', name: 'github', details: ['image changed'] }],
      summary: '1 to change',
    });
    render(<SpecDiffModal onApply={() => undefined} />);
    const dialog = screen.getByRole('dialog', { name: /reload config/i });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByRole('button', { name: /^close$/i })).toHaveAttribute('type', 'button');
  });
});
