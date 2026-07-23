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

vi.mock('../stores/useUIStore', () => ({
  useUIStore: vi.fn((selector: (s: object) => unknown) =>
    selector({ activeWorkspace: 'stack' }),
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
      const recent = makeCommand({ id: 'navigate:vault', label: 'Open Vault' });
      mockGetRecentCommands.mockReturnValue([recent]);
      render(<CommandPalette isOpen={true} onClose={onClose} />);
      expect(screen.getByText('Open Vault')).toBeInTheDocument();
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
      const cmd = makeCommand({ id: 'navigate:vault', label: 'Open Vault' });
      mockGetSortedCommands.mockImplementation((query?: string) =>
        query === 'vault' ? [cmd] : [],
      );
      render(<CommandPalette isOpen={true} onClose={onClose} />);

      fireEvent.change(screen.getByRole('combobox'), { target: { value: 'vault' } });

      expect(screen.getByText('Open Vault')).toBeInTheDocument();
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
      const cmd = makeCommand({ id: 'navigate:vault', label: 'Open Vault' });
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
