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
