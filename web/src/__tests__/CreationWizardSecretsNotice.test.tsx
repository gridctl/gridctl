import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import * as apiModule from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn(), ToastContainer: () => null }));

const NOTICE = 'Literal secrets are not kept across a reload. Re-enter them or use a stored variable.';

beforeEach(() => {
  cleanup();
  vi.restoreAllMocks();
  useWizardStore.getState().reset();
  vi.spyOn(apiModule, 'fetchWizardDrafts').mockResolvedValue([]);
  vi.spyOn(apiModule, 'validateStackSpec').mockResolvedValue({ valid: true, issues: [] } as never);
  vi.spyOn(apiModule, 'validateStackResource').mockResolvedValue({ valid: true, issues: [] } as never);
});

function renderForm(secretsRedacted: boolean) {
  useWizardStore.setState({ isOpen: true, currentStep: 'form', selectedType: 'mcp-server', secretsRedacted });
  render(<CreationWizard />);
}

describe('CreationWizard redacted secrets notice', () => {
  it('shows the notice when literals were dropped and dismisses it', () => {
    renderForm(true);
    expect(screen.getByText(NOTICE)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Dismiss notice' }));
    expect(screen.queryByText(NOTICE)).not.toBeInTheDocument();
    expect(useWizardStore.getState().secretsRedacted).toBe(false);
  });

  it('shows no notice when nothing was dropped', () => {
    renderForm(false);
    expect(screen.queryByText(NOTICE)).not.toBeInTheDocument();
  });
});
