import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { DraftManager } from '../components/wizard/DraftManager';
import { useWizardStore } from '../stores/useWizardStore';
import { deleteWizardDraft, fetchWizardDrafts } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../lib/api', () => ({
  fetchWizardDrafts: vi.fn(),
  saveWizardDraft: vi.fn(),
  deleteWizardDraft: vi.fn(),
}));

const draft = {
  id: 'd1',
  name: 'My draft',
  resourceType: 'mcp-server',
  formData: {},
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-01T00:00:00Z',
};

describe('DraftManager', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWizardStore.setState({ drafts: [], draftsLoading: false, selectedType: 'mcp-server' });
    vi.mocked(fetchWizardDrafts).mockResolvedValue([draft]);
    vi.mocked(deleteWizardDraft).mockResolvedValue(undefined);
  });

  it('confirms before deleting a draft', async () => {
    render(<DraftManager />);
    fireEvent.click(await screen.findByRole('button', { name: /drafts/i }));

    fireEvent.click(await screen.findByRole('button', { name: 'Delete draft My draft' }));
    expect(deleteWizardDraft).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/My draft/);
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/mcp-server/);

    fireEvent.click(screen.getByRole('button', { name: 'Delete "My draft"' }));
    await waitFor(() => expect(deleteWizardDraft).toHaveBeenCalledWith('d1'));
  });
});
