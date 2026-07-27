import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SkillFileTree } from '../SkillFileTree';
import { sortSkillFiles } from '../../../lib/skillFiles';
import { fetchSkillFiles } from '../../../lib/api';
import type { SkillFile } from '../../../types';

vi.mock('../../ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../../../lib/api', () => ({
  fetchSkillFiles: vi.fn(),
  writeSkillFile: vi.fn(),
  deleteSkillFile: vi.fn(),
}));

// Deliberately returned out of every useful order: fetch order is the backend
// walk, which is what the tree used to render verbatim.
const FILES: SkillFile[] = [
  { path: 'scripts/zebra.sh', size: 120, isDir: false },
  { path: 'scripts/alpha.sh', size: 9000, isDir: false },
  { path: 'scripts/middle.sh', size: 4000, isDir: false },
];

function fileNames(): string[] {
  return screen
    .getAllByRole('button')
    .map((b) => b.textContent ?? '')
    .filter((t) => t.endsWith('.sh'));
}

describe('sortSkillFiles', () => {
  it('orders by basename for the name axis', () => {
    expect(sortSkillFiles(FILES, 'name').map((f) => f.path)).toEqual([
      'scripts/alpha.sh',
      'scripts/middle.sh',
      'scripts/zebra.sh',
    ]);
  });

  it('orders largest first for the size axis', () => {
    expect(sortSkillFiles(FILES, 'size').map((f) => f.path)).toEqual([
      'scripts/alpha.sh',
      'scripts/middle.sh',
      'scripts/zebra.sh',
    ]);
  });

  it('breaks size ties by path so equal sizes stay stable', () => {
    const tied: SkillFile[] = [
      { path: 'scripts/b.sh', size: 10, isDir: false },
      { path: 'scripts/a.sh', size: 10, isDir: false },
    ];
    expect(sortSkillFiles(tied, 'size').map((f) => f.path)).toEqual(['scripts/a.sh', 'scripts/b.sh']);
  });

  it('orders by full path for the path axis', () => {
    const nested: SkillFile[] = [
      { path: 'references/b/one.md', size: 1, isDir: false },
      { path: 'references/a/one.md', size: 1, isDir: false },
    ];
    expect(sortSkillFiles(nested, 'path').map((f) => f.path)).toEqual([
      'references/a/one.md',
      'references/b/one.md',
    ]);
  });

  it('does not mutate its input', () => {
    const input = [...FILES];
    sortSkillFiles(input, 'size');
    expect(input.map((f) => f.path)).toEqual(FILES.map((f) => f.path));
  });
});

describe('SkillFileTree', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchSkillFiles).mockResolvedValue(FILES);
  });

  it('offers a sort control and defaults to name order', async () => {
    render(<SkillFileTree skillName="docx" readOnly />);
    expect(await screen.findByRole('group', { name: 'Sort files by' })).toBeInTheDocument();
    await waitFor(() => expect(fileNames()).toEqual(['alpha.sh', 'middle.sh', 'zebra.sh']));
  });

  it('marks the selected axis as pressed', async () => {
    render(<SkillFileTree skillName="docx" readOnly />);
    await screen.findByRole('group', { name: 'Sort files by' });

    expect(screen.getByRole('button', { name: 'Name' })).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(screen.getByRole('button', { name: 'Size' }));
    expect(screen.getByRole('button', { name: 'Size' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Name' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('puts the largest file first once size is selected', async () => {
    vi.mocked(fetchSkillFiles).mockResolvedValue([
      { path: 'scripts/aaa.sh', size: 1, isDir: false },
      { path: 'scripts/zzz.sh', size: 999, isDir: false },
    ]);
    render(<SkillFileTree skillName="docx" readOnly />);
    await waitFor(() => expect(fileNames()).toEqual(['aaa.sh', 'zzz.sh']));

    fireEvent.click(screen.getByRole('button', { name: 'Size' }));
    await waitFor(() => expect(fileNames()).toEqual(['zzz.sh', 'aaa.sh']));
  });

  it('omits the sort control when there is nothing to reorder', async () => {
    vi.mocked(fetchSkillFiles).mockResolvedValue([{ path: 'scripts/only.sh', size: 1, isDir: false }]);
    render(<SkillFileTree skillName="docx" readOnly />);
    await screen.findByText('only.sh');
    expect(screen.queryByRole('group', { name: 'Sort files by' })).not.toBeInTheDocument();
  });

  it('renders the empty state when a skill has no supporting files', async () => {
    vi.mocked(fetchSkillFiles).mockResolvedValue([]);
    render(<SkillFileTree skillName="docx" readOnly />);
    expect(await screen.findByText('No supporting files')).toBeInTheDocument();
  });
});
