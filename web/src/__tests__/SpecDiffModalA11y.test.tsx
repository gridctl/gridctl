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
    pendingSpec: null,
    appliedSpec: null,
    spec: null,
  });
});

describe('SpecDiffModal dialog semantics', () => {
  it('exposes a labeled modal dialog and a named close button', () => {
    useSpecStore.setState({
      diffModalOpen: true,
      diffModalMode: 'apply',
      pendingSpec: 'name: next\n',
      appliedSpec: { content: 'name: prev\n', path: 'stack.yaml' },
    });
    render(<SpecDiffModal onApply={() => undefined} />);
    const dialog = screen.getByRole('dialog', { name: /configuration changed/i });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByRole('button', { name: /^close$/i })).toHaveAttribute('type', 'button');
  });
});
