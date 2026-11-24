import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { EnvImportModal } from '../components/vault/EnvImportModal';
import type { ImportVariableInput, Variable } from '../lib/api';

const existing: Variable[] = [
  { key: 'A', type: 'string', is_secret: true },
  { key: 'B', type: 'string', is_secret: true },
];

const text = ['A=1', 'B=2', 'C=3', 'D=4', 'E=5'].join('\n');

function renderModal() {
  const onImport = vi
    .fn<(vars: ImportVariableInput[]) => Promise<{ imported: number }>>()
    .mockResolvedValue({ imported: 0 });
  render(
    <EnvImportModal
      onClose={vi.fn()}
      onImport={onImport}
      existingVariables={existing}
      sets={[]}
      initialText={text}
    />,
  );
  return { onImport };
}

const skip = (key: string) =>
  screen.getByRole('checkbox', { name: `Skip ${key}` }) as HTMLInputElement;
const skipAllConflicts = () =>
  screen.getByRole('checkbox', { name: 'Skip all conflicts' }) as HTMLInputElement;

describe('EnvImportModal — existing keys', () => {
  it('starts conflicting rows skipped and labels the button with new rows only', () => {
    renderModal();
    expect(skip('A')).toBeChecked();
    expect(skip('B')).toBeChecked();
    expect(skip('C')).not.toBeChecked();
    expect(skip('D')).not.toBeChecked();
    expect(skip('E')).not.toBeChecked();
    expect(screen.getAllByText('exists', { selector: 'td span' })).toHaveLength(2);
    expect(screen.queryByText('overwrites', { selector: 'td span' })).toBeNull();
    expect(skipAllConflicts()).toBeChecked();
    expect(
      screen.getByText(
        '2 keys already exist. Skipped rows are not written. Clear Skip on a row to overwrite it.',
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Import 3' })).toBeEnabled();
  });

  it('imports exactly the new keys by default', async () => {
    const { onImport } = renderModal();
    fireEvent.click(screen.getByRole('button', { name: 'Import 3' }));
    await waitFor(() => expect(onImport).toHaveBeenCalledTimes(1));
    expect(onImport.mock.calls[0][0].map((v) => v.key)).toEqual(['C', 'D', 'E']);
  });

  it('names the overwrites once conflicting rows are un-skipped', () => {
    renderModal();
    fireEvent.click(skip('A'));
    fireEvent.click(skip('B'));
    expect(screen.getAllByText('overwrites', { selector: 'td span' })).toHaveLength(2);
    expect(screen.getByRole('button', { name: 'Import 3, overwrite 2' })).toBeInTheDocument();
  });

  it('reads Overwrite N when only conflicting rows are active', () => {
    renderModal();
    fireEvent.click(skip('A'));
    fireEvent.click(skip('B'));
    fireEvent.click(skip('C'));
    fireEvent.click(skip('D'));
    fireEvent.click(skip('E'));
    expect(screen.getByRole('button', { name: 'Overwrite 2' })).toBeInTheDocument();
  });

  it('toggles every conflict with Skip all conflicts', () => {
    renderModal();
    fireEvent.click(skipAllConflicts());
    expect(skip('A')).not.toBeChecked();
    expect(skip('B')).not.toBeChecked();
    expect(skipAllConflicts()).not.toBeChecked();
    expect(screen.getByRole('button', { name: 'Import 3, overwrite 2' })).toBeInTheDocument();

    fireEvent.click(skipAllConflicts());
    expect(skip('A')).toBeChecked();
    expect(skip('B')).toBeChecked();
    expect(screen.getByRole('button', { name: 'Import 3' })).toBeInTheDocument();
  });
});

describe('EnvImportModal — discard confirm', () => {
  function renderPlain(initialText = '') {
    const onClose = vi.fn();
    const onImport = vi
      .fn<(vars: ImportVariableInput[]) => Promise<{ imported: number }>>()
      .mockResolvedValue({ imported: 1 });
    const view = render(
      <EnvImportModal
        onClose={onClose}
        onImport={onImport}
        existingVariables={existing}
        sets={[]}
        initialText={initialText}
      />,
    );
    return { onClose, onImport, view };
  }

  const textarea = () =>
    screen.getByRole('textbox', { name: 'Environment file content' }) as HTMLTextAreaElement;
  const backdrop = (container: HTMLElement) =>
    container.querySelector('.absolute.inset-0') as HTMLElement;

  it('closes a pristine modal on Escape with no confirm', () => {
    const { onClose } = renderPlain();
    fireEvent.keyDown(textarea(), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('confirms Escape on typed text; Keep editing keeps it and Discard closes', () => {
    const { onClose } = renderPlain();
    fireEvent.change(textarea(), { target: { value: 'NEW=1' } });
    fireEvent.keyDown(textarea(), { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog', { name: 'Discard import' })).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(textarea().value).toBe('NEW=1');
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.keyDown(textarea(), { key: 'Escape' });
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('ignores a second dismiss while the confirm is open', () => {
    const { onClose } = renderPlain();
    fireEvent.change(textarea(), { target: { value: 'NEW=1' } });
    fireEvent.keyDown(textarea(), { key: 'Escape' });
    fireEvent.click(screen.getByRole('button', { name: 'Close import' }));
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getAllByRole('alertdialog')).toHaveLength(1);
  });

  it.each([
    ['the backdrop', (c: HTMLElement) => fireEvent.click(backdrop(c))],
    ['the close button', () => fireEvent.click(screen.getByRole('button', { name: 'Close import' }))],
    ['Cancel', () => fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))],
  ])('confirms %s on typed text', (_name, dismiss) => {
    const { onClose, view } = renderPlain();
    fireEvent.change(textarea(), { target: { value: 'NEW=1' } });
    dismiss(view.container);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog', { name: 'Discard import' })).toBeInTheDocument();
  });

  it('places the confirm inside the modal panel', () => {
    renderPlain();
    fireEvent.change(textarea(), { target: { value: 'NEW=1' } });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    const panel = screen.getByRole('dialog', { name: 'Import variables' });
    expect(panel).toContainElement(screen.getByRole('alertdialog'));
  });

  it('closes a seeded, untouched modal on Cancel at once', () => {
    const { onClose } = renderPlain('A=1\nB=2');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('does not count revealing a value as a change', () => {
    const { onClose } = renderPlain('A=1\nB=2');
    fireEvent.click(screen.getAllByRole('button', { name: 'Reveal value' })[0]);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('counts a skip choice as a change', () => {
    const { onClose } = renderPlain('A=1\nB=2');
    fireEvent.click(skip('A'));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog', { name: 'Discard import' })).toBeInTheDocument();
  });

  it('counts a set choice as a change', () => {
    const onClose = vi.fn();
    render(
      <EnvImportModal
        onClose={onClose}
        onImport={vi.fn()}
        existingVariables={[]}
        sets={[{ name: 'dev', count: 0 }]}
        initialText="C=3"
      />,
    );
    fireEvent.change(screen.getByRole('combobox', { name: 'Variable set' }), {
      target: { value: 'dev' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  });

  it('closes after a successful import with no confirm', async () => {
    const { onClose, onImport } = renderPlain('C=3');
    fireEvent.change(textarea(), { target: { value: 'C=3\nD=4' } });
    fireEvent.click(screen.getByRole('button', { name: 'Import 2' }));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    expect(onImport).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('warns on reload while dirty and not while pristine', () => {
    renderPlain('A=1');
    const pristine = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(pristine);
    expect(pristine.defaultPrevented).toBe(false);
    fireEvent.change(textarea(), { target: { value: 'A=2' } });
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);
  });
});
