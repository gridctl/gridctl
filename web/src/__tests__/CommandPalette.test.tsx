import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { CommandPalette } from '../components/palette/CommandPalette';
import type { PaletteCommand } from '../types/palette';

// --- Mocks ---

const mockRecordUsage = vi.fn();
const mockGetSortedCommands = vi.fn();
const mockGetRecentCommands = vi.fn();

vi.mock('../hooks/useCommandRegistry', () => ({
  useCommandRegistry: () => ({
    getSortedCommands: mockGetSortedCommands,
    getRecentCommands: mockGetRecentCommands,
    recordUsage: mockRecordUsage,
  }),
}));

const uiState = vi.hoisted(() => ({
  activeWorkspace: 'stack',
  inspectorFocusRequest: null as string | null,
}));

vi.mock('../stores/useUIStore', () => ({
  useUIStore: Object.assign(
    vi.fn((selector: (s: object) => unknown) => selector(uiState)),
    { getState: () => uiState },
  ),
}));

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

// Import after vi.mock so we get the mocked version
import { showToast } from '../components/ui/Toast';

// --- Helpers ---

function makeCommand(overrides: Partial<PaletteCommand> = {}): PaletteCommand {
  return {
    id: 'navigate:traces',
    label: 'Open Traces',
    section: 'global',
    onSelect: vi.fn(),
    ...overrides,
  };
}

/**
 * Returns a getSortedCommands mock that only serves nav commands (no query, no scope).
 * Context commands (scope='canvas') and search return empty.
 */
function mockNavOnly(cmd: PaletteCommand) {
  return (query?: string, scope?: string) => (!query && !scope ? [cmd] : []);
}

// --- Tests ---

describe('CommandPalette', () => {
  const onClose = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    uiState.inspectorFocusRequest = null;
    mockGetRecentCommands.mockReturnValue([]);
    mockGetSortedCommands.mockReturnValue([]);
  });

  // ── Visibility ────────────────────────────────────────────────────────────

  describe('visibility', () => {
    it('renders nothing when isOpen is false', () => {
      render(<CommandPalette isOpen={false} onClose={onClose} />);
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    });

    it('renders the dialog when isOpen is true', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('has aria-modal="true" on the dialog', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true');
    });

    it('has aria-label="Command palette" on the dialog', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByRole('dialog')).toHaveAttribute('aria-label', 'Command palette');
    });
  });

  // ── ARIA ──────────────────────────────────────────────────────────────────

  describe('ARIA attributes', () => {
    it('has aria-live="polite" region for result count announcements', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(document.querySelector('[aria-live="polite"]')).toBeInTheDocument();
    });

    it('has role="combobox" on the search input', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByRole('combobox')).toBeInTheDocument();
    });

    it('has role="listbox" on the results list', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByRole('listbox')).toBeInTheDocument();
    });
  });

  // ── Keyboard ──────────────────────────────────────────────────────────────

  describe('keyboard interaction', () => {
    it('calls onClose when Escape is pressed', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      fireEvent.keyDown(document, { key: 'Escape' });
      expect(onClose).toHaveBeenCalledOnce();
    });

    it('does not call onClose for non-Escape keys', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      fireEvent.keyDown(document, { key: 'Enter' });
      expect(onClose).not.toHaveBeenCalled();
    });

    it('calls onClose (enabling focus restoration) when Escape closes the palette', () => {
      // Focus restoration itself runs inside requestAnimationFrame after onClose fires.
      // We verify onClose is called with the correct trigger — the caller restores focus.
      const button = document.createElement('button');
      document.body.appendChild(button);
      button.focus();

      render(<CommandPalette isOpen={true} onClose={onClose} />);
      fireEvent.keyDown(document, { key: 'Escape' });

      expect(onClose).toHaveBeenCalledOnce();
      document.body.removeChild(button);
    });
  });

  // ── Backdrop ──────────────────────────────────────────────────────────────

  describe('backdrop', () => {
    it('calls onClose when the backdrop is clicked', () => {
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      const dialog = screen.getByRole('dialog');
      fireEvent.click(dialog);
      expect(onClose).toHaveBeenCalledOnce();
    });
  });

  // ── Command display ───────────────────────────────────────────────────────

  describe('command display', () => {
    it('shows navigate commands in the default (non-search) state', () => {
      const cmd = makeCommand({ id: 'navigate:traces', label: 'Open Traces' });
      mockGetSortedCommands.mockImplementation(mockNavOnly(cmd));
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByText('Open Traces')).toBeInTheDocument();
    });

    it('shows recent commands when the registry returns them', () => {
      const recent = makeCommand({ id: 'navigate:vault', label: 'Open Variables' });
      mockGetRecentCommands.mockReturnValue([recent]);
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByText('Open Variables')).toBeInTheDocument();
    });
  });

  // ── Group headings ────────────────────────────────────────────────────────

  describe('group headings', () => {
    const labels = ['Recent', 'Current Context', 'Navigate To'];

    function renderAllGroups() {
      const recent = makeCommand({ id: 'navigate:vault', label: 'Open Variables' });
      const context = makeCommand({ id: 'node:github', label: 'Inspect github', section: 'canvas' });
      const nav = makeCommand({ id: 'navigate:traces', label: 'Open Traces' });
      mockGetRecentCommands.mockReturnValue([recent]);
      mockGetSortedCommands.mockImplementation((query?: string, scope?: string) => {
        if (query) return [];
        if (scope === 'canvas') return [context];
        if (!scope) return [nav];
        return [];
      });
      render(<CommandPalette isOpen={true} onClose={onClose} />);
    }

    it('renders each group heading exactly once', () => {
      renderAllGroups();
      for (const label of labels) {
        expect(screen.getAllByText(label)).toHaveLength(1);
      }
      expect(document.querySelectorAll('[cmdk-group-heading]')).toHaveLength(3);
    });

    it('names each group with its heading text', () => {
      renderAllGroups();
      for (const label of labels) {
        expect(screen.getByRole('group', { name: label })).toBeInTheDocument();
      }
      const names = Array.from(document.querySelectorAll('[cmdk-group-items]')).map((items) => {
        const id = items.getAttribute('aria-labelledby');
        expect(id).toBeTruthy();
        return document.getElementById(id!)?.textContent?.trim();
      });
      expect(names).toEqual(labels);
    });
  });

  // ── Command selection ─────────────────────────────────────────────────────

  describe('command selection', () => {
    it('calls recordUsage and onSelect when a command is clicked', () => {
      const onSelectFn = vi.fn();
      const cmd = makeCommand({ id: 'navigate:traces', label: 'Open Traces', onSelect: onSelectFn });
      mockGetSortedCommands.mockImplementation(mockNavOnly(cmd));
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.click(screen.getByText('Open Traces'));

      expect(mockRecordUsage).toHaveBeenCalledWith('navigate:traces');
      expect(onSelectFn).toHaveBeenCalledOnce();
      expect(onClose).toHaveBeenCalledOnce();
    });

    it('leaves focus to the inspector after a command that requests it', () => {
      const frames: FrameRequestCallback[] = [];
      const raf = vi.spyOn(globalThis, 'requestAnimationFrame').mockImplementation((cb) => {
        frames.push(cb);
        return frames.length;
      });
      const outside = document.createElement('button');
      const heading = document.createElement('h2');
      heading.tabIndex = -1;
      document.body.append(outside, heading);
      outside.focus();
      const cmd = makeCommand({
        id: 'node:github',
        label: 'Inspect github',
        onSelect: () => {
          uiState.inspectorFocusRequest = 'mcp-github';
        },
      });
      mockGetSortedCommands.mockImplementation((query?: string, scope?: string) =>
        !query && scope === 'canvas' ? [cmd] : [],
      );
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.click(screen.getByText('Inspect github'));
      // Stands in for the Sidebar effect, which focuses the heading after the
      // command commits and before the palette's restore frame would run.
      heading.focus();
      frames.forEach((cb) => cb(0));

      expect(onClose).toHaveBeenCalledOnce();
      expect(document.activeElement).toBe(heading);
      raf.mockRestore();
      outside.remove();
      heading.remove();
    });

    it('shows a toast and does not call onSelect for unavailable commands', () => {
      const onSelectFn = vi.fn();
      const cmd = makeCommand({
        id: 'navigate:traces',
        label: 'Open Traces',
        onSelect: onSelectFn,
        unavailable: true,
      });
      mockGetSortedCommands.mockImplementation(mockNavOnly(cmd));
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.click(screen.getByText('Open Traces'));

      expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('unavailable'));
      expect(onSelectFn).not.toHaveBeenCalled();
      expect(mockRecordUsage).not.toHaveBeenCalled();
      expect(onClose).toHaveBeenCalledOnce();
    });

    it('shows "unavailable" label on unavailable commands', () => {
      const cmd = makeCommand({ id: 'navigate:traces', label: 'Open Traces', unavailable: true });
      mockGetSortedCommands.mockImplementation(mockNavOnly(cmd));
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByText('unavailable')).toBeInTheDocument();
    });
  });

  // ── Search ────────────────────────────────────────────────────────────────

  describe('search', () => {
    it('shows search results when typing in the input', () => {
      const cmd = makeCommand({ id: 'navigate:vault', label: 'Open Variables' });
      mockGetSortedCommands.mockImplementation((query?: string) =>
        query === 'vault' ? [cmd] : [],
      );
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'vault' } });

      expect(screen.getByText('Open Variables')).toBeInTheDocument();
    });

    it('shows empty state when search has no results', () => {
      mockGetSortedCommands.mockReturnValue([]);
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'xyznotfound' } });

      expect(screen.getByText(/No results for/)).toBeInTheDocument();
    });

    it('empty state includes scope filter hints', () => {
      mockGetSortedCommands.mockReturnValue([]);
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'xyznotfound' } });

      const hints = screen.getByText(/for actions/);
      expect(hints).toBeInTheDocument();
    });

    it('announces result count via aria-live region', () => {
      const cmd = makeCommand({ id: 'navigate:vault', label: 'Open Variables' });
      mockGetSortedCommands.mockImplementation((query?: string) =>
        query ? [cmd] : [],
      );
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'vault' } });

      const liveRegion = document.querySelector('[aria-live="polite"]');
      expect(liveRegion?.textContent).toMatch(/1 result/);
    });

    it('announces "No results" when search returns empty', () => {
      mockGetSortedCommands.mockReturnValue([]);
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'xyznotfound' } });

      const liveRegion = document.querySelector('[aria-live="polite"]');
      expect(liveRegion?.textContent).toBe('No results');
    });
  });
});
