import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { DraftManager } from '../components/wizard/DraftManager';
import { useWizardStore } from '../stores/useWizardStore';
import { deleteWizardDraft, fetchWizardDrafts, saveWizardDraft } from '../lib/api';
import { showToast } from '../components/ui/Toast';

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

  it('drops literal secrets from the form data it sends', async () => {
    useWizardStore.getState().updateFormData('mcp-server', {
      serverType: 'external',
      auth: { type: 'bearer', token: 'sk-literal-123' },
    });
    const arg = await save('secret one');
    expect((arg.formData.auth as { token: string }).token).toBe('');
    expect(JSON.stringify(arg)).not.toContain('sk-literal-123');
    expect(useWizardStore.getState().formData['mcp-server'].auth?.token).toBe('sk-literal-123');
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('success', 'Draft "secret one" saved without 1 literal secret'),
    );
  });

  it('keeps references in a saved draft', async () => {
    useWizardStore.getState().updateFormData('mcp-server', {
      serverType: 'external',
      auth: { type: 'bearer', token: '${var:TOKEN}' },
    });
    const arg = await save('ref one');
    expect((arg.formData.auth as { token: string }).token).toBe('${var:TOKEN}');
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Draft "ref one" saved'));
  });

  it('masks literals in YAML-mode text and counts them', async () => {
    useWizardStore.setState({ expertMode: true, yamlContent: 'name: x\nauth:\n  type: bearer\n  token: sk-literal-123\n' });
    const arg = await save('yaml secret');
    expect(arg.yaml).toBe('name: x\nauth:\n  type: bearer\n  token: ""\n');
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('success', 'Draft "yaml secret" saved without 1 literal secret'),
    );
  });

  it('omits YAML-mode text that does not parse and warns', async () => {
    useWizardStore.setState({ expertMode: true, yamlContent: 'auth:\n  token: sk-literal-123\n  bad: [\n' });
    const arg = await save('broken');
    expect('yaml' in arg).toBe(false);
    expect(JSON.stringify(arg)).not.toContain('sk-literal-123');
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('warning', expect.stringContaining('without its YAML')));
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

describe('DraftManager popover dismissal', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useWizardStore.getState().reset();
    useWizardStore.setState({ drafts: [], draftsLoading: false, selectedType: 'mcp-server' });
    vi.mocked(fetchWizardDrafts).mockResolvedValue([draft]);
  });

  it('labels the triggers and popovers', async () => {
    render(<DraftManager />);
    const save = screen.getByRole('button', { name: /save/i });
    const list = screen.getByRole('button', { name: /drafts/i });
    for (const trigger of [save, list]) {
      expect(trigger).toHaveAttribute('type', 'button');
      expect(trigger).toHaveAttribute('aria-haspopup', 'dialog');
      expect(trigger).toHaveAttribute('aria-expanded', 'false');
    }
    fireEvent.click(save);
    expect(save).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('dialog', { name: 'Save draft' })).toBeInTheDocument();
    fireEvent.click(list);
    expect(list).toHaveAttribute('aria-expanded', 'true');
    expect(await screen.findByRole('dialog', { name: 'Saved drafts' })).toBeInTheDocument();
  });

  it('closes the Save popover on Escape and returns focus to its trigger', () => {
    render(<DraftManager />);
    const save = screen.getByRole('button', { name: /save/i });
    fireEvent.click(save);
    const input = screen.getByLabelText('Draft name');
    input.focus();
    fireEvent.keyDown(input, { key: 'Escape' });
    expect(screen.queryByLabelText('Draft name')).not.toBeInTheDocument();
    expect(save).toHaveFocus();
  });

  it('closes only the delete confirm on Escape, keeping the list', async () => {
    render(<DraftManager />);
    fireEvent.click(screen.getByRole('button', { name: /drafts/i }));
    fireEvent.click(await screen.findByRole('button', { name: 'Delete draft My draft' }));
    const confirm = screen.getByRole('alertdialog');
    const inside = within(confirm).getAllByRole('button')[0];
    inside.focus();
    fireEvent.keyDown(inside, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByRole('dialog', { name: 'Saved drafts' })).toBeInTheDocument();
  });

  it('closes an open popover on an outside click', async () => {
    render(<DraftManager />);
    fireEvent.click(screen.getByRole('button', { name: /drafts/i }));
    expect(await screen.findByRole('dialog', { name: 'Saved drafts' })).toBeInTheDocument();
    fireEvent.mouseDown(document.body);
    expect(screen.queryByRole('dialog', { name: 'Saved drafts' })).not.toBeInTheDocument();
  });
});
