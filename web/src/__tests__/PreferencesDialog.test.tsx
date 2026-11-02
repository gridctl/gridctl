import { describe, expect, it, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { PreferencesDialog } from '../components/shell/PreferencesDialog';
import { ShortcutsPanel } from '../components/shell/ShortcutsPanel';
import { useTextZoom } from '../hooks/useTextZoom';
import { LIBRARY_TEXT_ZOOM } from '../lib/textZoomDefaults';
import { COMPACT_MODE_DEFAULTS, useUIStore } from '../stores/useUIStore';
import { WORKSPACE_CONFIG } from '../types/workspace';
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts';

function ZoomConsumer() {
  const { fontSize } = useTextZoom({
    storageKey: LIBRARY_TEXT_ZOOM.name,
    defaultSize: LIBRARY_TEXT_ZOOM.defaultSize,
    minSize: LIBRARY_TEXT_ZOOM.minSize,
    maxSize: LIBRARY_TEXT_ZOOM.maxSize,
  });
  return <span data-testid="library-zoom">{fontSize}</span>;
}

function ShortcutHarness() {
  const setShortcutsOpen = useUIStore((s) => s.setShortcutsOpen);
  useKeyboardShortcuts({ onOpenShortcuts: () => setShortcutsOpen(true) });
  return (
    <>
      <ShortcutsPanel />
      <input aria-label="Search field" />
      <div role="dialog">
        <button type="button">Inside dialog</button>
      </div>
    </>
  );
}

describe('PreferencesDialog', () => {
  beforeEach(() => {
    localStorage.clear();
    useUIStore.setState({
      preferencesOpen: true,
      shortcutsOpen: false,
      compactMode: { ...COMPACT_MODE_DEFAULTS },
      themeMode: 'system',
    });
  });

  it('offers compact switches only for workspaces that read the flag', () => {
    render(<PreferencesDialog />);
    const shown = ['Library', 'Variables', 'Tools', 'Metrics', 'Pins', 'Logs', 'Traces'];
    for (const label of shown) {
      expect(screen.getByRole('switch', { name: label })).toBeInTheDocument();
    }
    expect(screen.queryByRole('switch', { name: 'Stack' })).not.toBeInTheDocument();
    expect(screen.queryByRole('switch', { name: 'Connections' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('switch')).toHaveLength(7);
  });

  it('toggles compact mode and updates a mounted text-size consumer', () => {
    render(
      <>
        <ZoomConsumer />
        <PreferencesDialog />
      </>,
    );
    expect(screen.getByTestId('library-zoom')).toHaveTextContent('13');
    fireEvent.click(screen.getByRole('switch', { name: 'Library' }));
    expect(useUIStore.getState().compactMode.library).toBe(true);
    expect(useUIStore.getState().compactMode.tools).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: 'Increase Library text size' }));
    expect(screen.getByTestId('library-zoom')).toHaveTextContent('14');
    expect(screen.getByText('14px')).toBeInTheDocument();
  });

  it('lists a text-size row for Library, Tools, Traces, and Logs', () => {
    render(<PreferencesDialog />);
    expect(screen.getAllByText('Library').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Tools').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Traces').length).toBeGreaterThan(0);
    expect(screen.getByText('Log font size')).toBeInTheDocument();
    expect(screen.getByText(/Cmd\+Backslash toggles compact mode/)).toBeInTheDocument();
  });
});

describe('ShortcutsPanel', () => {
  beforeEach(() => {
    useUIStore.setState({ shortcutsOpen: true, preferencesOpen: false });
  });

  it('lists the required chords once, with Global scope on the shell chords', () => {
    render(<ShortcutsPanel />);
    const required = ['f', '[', ']', 'n', 'e', 'd', 'j', 'k', '/', 'Enter', 'Escape', '⌘', '?', '⇧'];
    for (const key of required) {
      expect(screen.getAllByText(key).length).toBeGreaterThan(0);
    }
    for (const workspace of WORKSPACE_CONFIG) {
      expect(screen.getAllByText(workspace.label).length).toBeGreaterThan(0);
    }
    expect(screen.getAllByText('Global').length).toBeGreaterThan(0);
    expect(screen.queryByText('Stack', { selector: '.sr-only' })).not.toBeInTheDocument();
  });
});

describe('shortcut discovery', () => {
  beforeEach(() => {
    useUIStore.setState({ shortcutsOpen: false });
  });

  it('opens the panel on ? and ignores the key inside a field or dialog', () => {
    render(<ShortcutHarness />);
    fireEvent.keyDown(window, { key: '?' });
    expect(screen.getByRole('dialog', { name: 'Keyboard shortcuts' })).toBeInTheDocument();

    act(() => {
      useUIStore.setState({ shortcutsOpen: false });
    });
    expect(screen.queryByRole('dialog', { name: 'Keyboard shortcuts' })).not.toBeInTheDocument();

    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Search field' }), { key: '?' });
    expect(screen.queryByRole('dialog', { name: 'Keyboard shortcuts' })).not.toBeInTheDocument();

    fireEvent.keyDown(screen.getByRole('button', { name: 'Inside dialog' }), { key: '?' });
    expect(screen.queryByRole('dialog', { name: 'Keyboard shortcuts' })).not.toBeInTheDocument();
  });
});
