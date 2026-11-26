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
import { isModalOpen } from '../lib/modalPresence';

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

// Each prefix sits in a <code> followed by the word it scopes to.
function expectScopeHints(container: HTMLElement) {
  const text = container.textContent ?? '';
  expect(text).toMatch(/t:\s*traces/);
  expect(text).toMatch(/v:\s*variables/);
  expect(text).toMatch(/l:\s*library/);
  expect(text).toMatch(/>\s*actions/);
  expect(text).not.toContain('r:');
  const codes = Array.from(container.querySelectorAll('code')).map((c) => c.textContent);
  expect(codes).toEqual(expect.arrayContaining(['t:', 'v:', 'l:', '>']));
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

      expectScopeHints(screen.getByText(/No results for/).parentElement!);
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

describe('CommandPalette scope and hints', () => {
  const onClose = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    mockGetRecentCommands.mockReturnValue([]);
    mockGetSortedCommands.mockReturnValue([]);
  });

  function input() {
    return screen.getByRole('combobox') as HTMLInputElement;
  }

  it.each(['l:', 'r:'])('shows the Library chip and scopes to registry for %s', (prefix) => {
    render(<CommandPalette isOpen onClose={onClose} />);
    fireEvent.change(input(), { target: { value: `${prefix}abc` } });
    expect(screen.getByRole('button', { name: 'Clear scope filter' }).parentElement).toHaveTextContent('Library');
    expect(mockGetSortedCommands).toHaveBeenCalledWith('abc', 'registry', 'stack');
  });

  it('shows the hints with their words in the default empty state, the no-results state, and the footer', () => {
    const { container } = render(<CommandPalette isOpen onClose={onClose} />);
    expectScopeHints(screen.getByText('Start typing to search commands').parentElement!);
    const footer = screen.getByText('select').parentElement!.parentElement!;
    expectScopeHints(footer);
    fireEvent.change(input(), { target: { value: 'xyz' } });
    expectScopeHints(screen.getByText(/No results for/).parentElement!);
    expect(container.ownerDocument.body.textContent).not.toContain('r:');
  });

  it('cycles Tab between the input and the chip and clears on Enter without selecting', () => {
    const onSelect = vi.fn();
    const cmd = makeCommand({ id: 'trace:1', label: 'Search traces: x', section: 'traces', onSelect });
    mockGetSortedCommands.mockImplementation((_q?: string, scope?: string) => (scope === 'traces' ? [cmd] : []));
    render(<CommandPalette isOpen onClose={onClose} />);
    input().focus();
    fireEvent.change(input(), { target: { value: 't:' } });
    const clear = screen.getByRole('button', { name: 'Clear scope filter' });
    expect(clear).toHaveAttribute('type', 'button');

    fireEvent.keyDown(input(), { key: 'Tab' });
    expect(document.activeElement).toBe(clear);
    fireEvent.keyDown(clear, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(input());
    fireEvent.keyDown(input(), { key: 'Tab' });
    expect(document.activeElement).toBe(clear);

    fireEvent.keyDown(clear, { key: 'Enter' });
    expect(input().value).toBe('');
    expect(document.activeElement).toBe(input());
    expect(onSelect).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Clear scope filter' })).not.toBeInTheDocument();
  });

  it('clears on Space and click and returns focus to the input', () => {
    render(<CommandPalette isOpen onClose={onClose} />);
    fireEvent.change(input(), { target: { value: 'v:key' } });
    const clear = screen.getByRole('button', { name: 'Clear scope filter' });
    clear.focus();
    fireEvent.keyDown(clear, { key: ' ' });
    expect(input().value).toBe('');
    expect(document.activeElement).toBe(input());

    fireEvent.change(input(), { target: { value: 'v:key' } });
    fireEvent.click(screen.getByRole('button', { name: 'Clear scope filter' }));
    expect(input().value).toBe('');
    expect(document.activeElement).toBe(input());
  });

  it('keeps focus on the input for Tab with no scope', () => {
    render(<CommandPalette isOpen onClose={onClose} />);
    input().focus();
    fireEvent.keyDown(input(), { key: 'Tab' });
    expect(document.activeElement).toBe(input());
    fireEvent.keyDown(input(), { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(input());
  });

  it.each(['t:', 'v:', 'l:', 'r:', '>'])('removes a bare %s with one Backspace', (prefix) => {
    render(<CommandPalette isOpen onClose={onClose} />);
    fireEvent.change(input(), { target: { value: prefix } });
    fireEvent.keyDown(input(), { key: 'Backspace' });
    expect(input().value).toBe('');
  });

  it('renders at most 50 rows and announces the full count', () => {
    const many = Array.from({ length: 60 }, (_, i) =>
      makeCommand({ id: `cmd:${i}`, label: `Command ${i}` }),
    );
    mockGetSortedCommands.mockImplementation((query?: string) => (query ? many : []));
    render(<CommandPalette isOpen onClose={onClose} />);
    fireEvent.change(input(), { target: { value: 'command' } });
    expect(screen.getAllByRole('option')).toHaveLength(50);
    expect(screen.getByText('Showing 50 of 60. Keep typing to narrow.')).toBeInTheDocument();
    expect(document.querySelector('[aria-live="polite"]')?.textContent).toBe('60 results');
  });

  it.each([
    ['v:', 'No variables loaded yet. Open the Variables workspace to load them.'],
    ['t:', 'No traces loaded yet. Open the Traces workspace to load the buffer.'],
  ])('explains an empty %s scope with nothing loaded', (prefix, text) => {
    render(<CommandPalette isOpen onClose={onClose} />);
    fireEvent.change(input(), { target: { value: `${prefix}abc` } });
    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it('keeps the generic text when the scoped section has commands', () => {
    const cmd = makeCommand({ id: 'vault:A', label: 'Open variable: A', section: 'vault' });
    mockGetSortedCommands.mockImplementation((query?: string, scope?: string) =>
      !query && scope === 'vault' ? [cmd] : [],
    );
    render(<CommandPalette isOpen onClose={onClose} />);
    fireEvent.change(input(), { target: { value: 'v:zzz' } });
    expect(screen.getByText(/No results for/)).toBeInTheDocument();
    expect(screen.queryByText(/No variables loaded yet/)).not.toBeInTheDocument();
  });

  it('does not query the registry while closed', () => {
    render(<CommandPalette isOpen={false} onClose={onClose} />);
    expect(mockGetSortedCommands).not.toHaveBeenCalled();
    expect(mockGetRecentCommands).not.toHaveBeenCalled();
  });
});

describe('CommandPalette modal presence', () => {
  beforeEach(() => {
    mockGetRecentCommands.mockReturnValue([]);
    mockGetSortedCommands.mockReturnValue([]);
  });

  it('marks a modal open only while the palette is open', () => {
    const view = render(<CommandPalette isOpen={false} onClose={vi.fn()} />);
    expect(isModalOpen()).toBe(false);
    view.rerender(<CommandPalette isOpen onClose={vi.fn()} />);
    expect(isModalOpen()).toBe(true);
    view.rerender(<CommandPalette isOpen={false} onClose={vi.fn()} />);
    expect(isModalOpen()).toBe(false);
    view.rerender(<CommandPalette isOpen onClose={vi.fn()} />);
    expect(isModalOpen()).toBe(true);
    view.unmount();
    expect(isModalOpen()).toBe(false);
  });
});
