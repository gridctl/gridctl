import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { PackImportWizard } from '../components/wizard/steps/PackImportWizard';
import { SkillImportWizard } from '../components/wizard/steps/SkillImportWizard';
import { useWizardStore } from '../stores/useWizardStore';
import * as apiModule from '../lib/api';
import type { SkillPreview } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn(), ToastContainer: () => null }));

beforeEach(() => {
  cleanup();
  vi.restoreAllMocks();
  useWizardStore.getState().reset();
  vi.spyOn(apiModule, 'fetchWizardDrafts').mockResolvedValue([]);
  Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
    configurable: true,
    get() {
      return this.parentNode;
    },
  });
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (err: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function renderWizardOnType() {
  useWizardStore.setState({ isOpen: true, currentStep: 'type' });
  render(<CreationWizard />);
}

function backdrop(): HTMLElement {
  const dialog = screen.getByRole('dialog');
  return dialog.previousElementSibling as HTMLElement;
}

describe('CreationWizard dismiss guards', () => {
  const closePaths: [string, () => void][] = [
    ['backdrop click', () => fireEvent.click(backdrop())],
    ['Escape', () => fireEvent.keyDown(document, { key: 'Escape' })],
    ['Close button', () => fireEvent.click(screen.getByRole('button', { name: 'Close' }))],
  ];

  it.each(closePaths)('%s confirms while an import has progress', (_label, dismiss) => {
    renderWizardOnType();
    act(() => useWizardStore.getState().setImportInProgress(true));

    dismiss();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/Discard this import/);
    expect(useWizardStore.getState().isOpen).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useWizardStore.getState().isOpen).toBe(true);
    expect(useWizardStore.getState().importInProgress).toBe(true);

    dismiss();
    fireEvent.click(screen.getByRole('button', { name: 'Discard import' }));
    expect(useWizardStore.getState().isOpen).toBe(false);
  });

  it('Escape on the confirm cancels it without closing the wizard', () => {
    renderWizardOnType();
    act(() => useWizardStore.getState().setImportInProgress(true));
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useWizardStore.getState().isOpen).toBe(true);
  });

  it('closes immediately with no import progress', () => {
    renderWizardOnType();
    useWizardStore.getState().updateFormData('stack', { name: 'kept' });
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useWizardStore.getState().isOpen).toBe(false);
  });

  it('Start over confirms when the form is dirty', () => {
    renderWizardOnType();
    act(() => useWizardStore.getState().updateFormData('stack', { name: 'unsaved' }));
    fireEvent.click(screen.getByRole('button', { name: 'Start over' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/clears the form and any YAML edits/);

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(useWizardStore.getState().formData.stack.name).toBe('unsaved');

    fireEvent.click(screen.getByRole('button', { name: 'Start over' }));
    const dialog = screen.getByRole('alertdialog');
    fireEvent.click(dialog.querySelector('button:last-of-type') as HTMLElement);
    expect(useWizardStore.getState().formData.stack.name).toBe('');
  });

  it('Start over confirms while an import has progress', () => {
    renderWizardOnType();
    act(() => useWizardStore.getState().setImportInProgress(true));
    fireEvent.click(screen.getByRole('button', { name: 'Start over' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/discards the import in progress/);
  });

  it('Start over resets a clean wizard immediately', () => {
    useWizardStore.setState({ isOpen: true, currentStep: 'template', selectedType: 'stack' });
    render(<CreationWizard />);
    fireEvent.click(screen.getByRole('button', { name: 'Start over' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useWizardStore.getState().currentStep).toBe('type');
  });
});

const packPreview = {
  pack: 'team-pack',
  version: '1.0.0',
  wiring: false,
  skills: [],
  agents: [],
  rules: [],
};

describe('PackImportWizard import progress', () => {
  function renderPack() {
    return render(
      <MemoryRouter>
        <PackImportWizard />
      </MemoryRouter>,
    );
  }

  function preview() {
    fireEvent.change(screen.getByLabelText(/pack repository url/i), {
      target: { value: 'https://github.com/acme/team-pack' },
    });
    fireEvent.click(screen.getByRole('button', { name: /preview pack/i }));
  }

  it('publishes progress through preview, review, and done', async () => {
    const pending = deferred<typeof packPreview>();
    vi.spyOn(apiModule, 'previewPack').mockReturnValue(pending.promise as never);
    const addSpy = vi.spyOn(apiModule, 'addPack').mockResolvedValue({
      doc: { pack: 'team-pack' },
      notes: [],
    } as never);
    const apply = deferred<unknown>();
    vi.spyOn(apiModule, 'applyPack').mockReturnValue(apply.promise as never);

    const { unmount } = renderPack();
    expect(useWizardStore.getState().importInProgress).toBe(false);

    preview();
    await waitFor(() => expect(useWizardStore.getState().importInProgress).toBe(true));

    await act(async () => pending.resolve(packPreview));
    expect(await screen.findByRole('button', { name: /import pack/i })).toBeInTheDocument();
    expect(useWizardStore.getState().importInProgress).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: /back/i }));
    await waitFor(() => expect(useWizardStore.getState().importInProgress).toBe(false));

    vi.mocked(apiModule.previewPack).mockResolvedValue(packPreview as never);
    preview();
    fireEvent.click(await screen.findByRole('button', { name: /import pack/i }));
    await waitFor(() => expect(addSpy).toHaveBeenCalled());
    expect(await screen.findByRole('button', { name: /apply now/i })).toBeInTheDocument();
    expect(useWizardStore.getState().importInProgress).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: /apply now/i }));
    expect(screen.getByRole('button', { name: /applying/i })).toBeInTheDocument();
    expect(useWizardStore.getState().importInProgress).toBe(false);

    await act(async () => apply.resolve({}));
    unmount();
    expect(useWizardStore.getState().importInProgress).toBe(false);
  });

  it('clears progress on unmount', async () => {
    vi.spyOn(apiModule, 'previewPack').mockResolvedValue(packPreview as never);
    const { unmount } = renderPack();
    preview();
    await screen.findByRole('button', { name: /import pack/i });
    expect(useWizardStore.getState().importInProgress).toBe(true);
    unmount();
    expect(useWizardStore.getState().importInProgress).toBe(false);
  });

  it('raises the leave prompt while a pack import has progress', async () => {
    vi.spyOn(apiModule, 'previewPack').mockResolvedValue(packPreview as never);
    renderPack();
    const before = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(before);
    expect(before.defaultPrevented).toBe(false);

    preview();
    await screen.findByRole('button', { name: /import pack/i });
    const during = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(during);
    expect(during.defaultPrevented).toBe(true);
  });
});

function makeSkill(): SkillPreview {
  return {
    name: 'test-skill',
    description: 'A test skill',
    body: '# Test',
    valid: true,
    exists: false,
    errors: [],
    warnings: [],
    findings: [],
  } as SkillPreview;
}

describe('SkillImportWizard import progress', () => {
  beforeEach(() => {
    vi.spyOn(apiModule, 'fetchSkillSources').mockResolvedValue([]);
    vi.spyOn(apiModule, 'fetchRegistrySkills').mockResolvedValue([]);
    vi.spyOn(apiModule, 'fetchRegistryStatus').mockResolvedValue({} as never);
    vi.spyOn(apiModule, 'fetchRegistryAgents').mockResolvedValue([]);
    vi.spyOn(apiModule, 'fetchAgentProjectionStatus').mockResolvedValue([]);
  });

  function scan() {
    fireEvent.change(screen.getByPlaceholderText('https://github.com/owner/repo'), {
      target: { value: 'https://github.com/owner/repo' },
    });
    fireEvent.click(screen.getByText('Scan for Skills'));
  }

  it('is in progress while the preview is pending and on browse', async () => {
    const pending = deferred<unknown>();
    vi.spyOn(apiModule, 'previewSkillSource').mockReturnValue(pending.promise as never);
    render(<SkillImportWizard />);
    expect(useWizardStore.getState().importInProgress).toBe(false);

    scan();
    await waitFor(() => expect(useWizardStore.getState().importInProgress).toBe(true));

    await act(async () => pending.resolve({ skills: [makeSkill()] }));
    expect(await screen.findByRole('button', { name: /Install 1 Skill/ })).toBeInTheDocument();
    expect(useWizardStore.getState().importInProgress).toBe(true);
  });

  it('is not in progress after a failed preview', async () => {
    vi.spyOn(apiModule, 'previewSkillSource').mockRejectedValue(new Error('not found'));
    render(<SkillImportWizard />);
    scan();
    await waitFor(() => expect(apiModule.previewSkillSource).toHaveBeenCalled());
    await waitFor(() => expect(useWizardStore.getState().importInProgress).toBe(false));
  });

  it('keeps progress after a rejected install, which stays on browse for a retry', async () => {
    vi.spyOn(apiModule, 'previewSkillSource').mockResolvedValue({ skills: [makeSkill()] } as never);
    vi.spyOn(apiModule, 'addSkillSource').mockRejectedValue(new Error('clone failed'));
    render(<SkillImportWizard />);
    scan();
    fireEvent.click(await screen.findByRole('button', { name: /Install 1 Skill/ }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/clone failed/);
    expect(screen.getByRole('button', { name: /Install 1 Skill/ })).toBeInTheDocument();
    expect(useWizardStore.getState().importInProgress).toBe(true);
  });

  it('is not in progress after a finished install', async () => {
    vi.spyOn(apiModule, 'previewSkillSource').mockResolvedValue({ skills: [makeSkill()] } as never);
    vi.spyOn(apiModule, 'addSkillSource').mockResolvedValue({ imported: [{ name: 'test-skill' }] } as never);
    render(<SkillImportWizard />);
    scan();
    fireEvent.click(await screen.findByRole('button', { name: /Install 1 Skill/ }));
    expect(await screen.findByText('Import Complete')).toBeInTheDocument();
    expect(useWizardStore.getState().importInProgress).toBe(false);
  });
});
