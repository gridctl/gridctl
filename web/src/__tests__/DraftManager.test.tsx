import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { DraftManager } from '../components/wizard/DraftManager';
import { useWizardStore } from '../stores/useWizardStore';
import { deleteWizardDraft, fetchWizardDrafts, saveWizardDraft } from '../lib/api';

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

describe('DraftManager YAML and load guard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWizardStore.getState().reset();
    useWizardStore.setState({ drafts: [], draftsLoading: false, selectedType: 'mcp-server' });
    vi.mocked(fetchWizardDrafts).mockResolvedValue([draft]);
    vi.mocked(saveWizardDraft).mockResolvedValue({ ...draft, id: 'new' });
  });

  async function save(name: string) {
    render(<DraftManager />);
    fireEvent.click(screen.getByRole('button', { name: /save/i }));
    fireEvent.change(screen.getByLabelText('Draft name'), { target: { value: name } });
    fireEvent.click(screen.getAllByRole('button', { name: 'Save' }).at(-1)!);
    await waitFor(() => expect(saveWizardDraft).toHaveBeenCalled());
    return vi.mocked(saveWizardDraft).mock.calls[0][0];
  }

  it('sends the YAML text with a draft saved in YAML mode', async () => {
    useWizardStore.setState({ expertMode: true, yamlContent: 'name: x\nrestart: always\n' });
    const arg = await save('yaml one');
    expect(arg.yaml).toBe('name: x\nrestart: always\n');
    expect(arg.formData).toBeDefined();
  });

  it('omits yaml for a draft saved in Form mode', async () => {
    useWizardStore.setState({ expertMode: false, yamlContent: 'name: x\n' });
    const arg = await save('form one');
    expect('yaml' in arg).toBe(false);
  });

  it('confirms before loading a draft over dirty form data', async () => {
    useWizardStore.getState().updateFormData('stack', { name: 'unsaved' });
    render(<DraftManager />);
    fireEvent.click(await screen.findByRole('button', { name: /drafts/i }));
    fireEvent.click(await screen.findByText('My draft'));

    expect(screen.getByRole('alertdialog')).toHaveTextContent(/My draft/);
    expect(useWizardStore.getState().formData.stack.name).toBe('unsaved');
    expect(useWizardStore.getState().currentStep).toBe('type');

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useWizardStore.getState().currentStep).toBe('type');

    fireEvent.click(screen.getByText('My draft'));
    fireEvent.click(screen.getByRole('button', { name: 'Load "My draft"' }));
    expect(useWizardStore.getState().currentStep).toBe('form');
    expect(useWizardStore.getState().selectedType).toBe('mcp-server');
  });

  it('confirms before loading a draft while an import has progress', async () => {
    useWizardStore.getState().setImportInProgress(true);
    render(<DraftManager />);
    fireEvent.click(await screen.findByRole('button', { name: /drafts/i }));
    fireEvent.click(await screen.findByText('My draft'));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  });

  it('loads immediately into a clean wizard', async () => {
    render(<DraftManager />);
    fireEvent.click(await screen.findByRole('button', { name: /drafts/i }));
    fireEvent.click(await screen.findByText('My draft'));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useWizardStore.getState().currentStep).toBe('form');
  });
});
