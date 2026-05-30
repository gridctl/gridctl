import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { AccessLens } from '../components/topology/AccessLens';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import type { ClientStatus, MCPServerStatus } from '../types';

const SERVERS: MCPServerStatus[] = [
  { name: 'github', transport: 'http', initialized: true, toolCount: 2, tools: ['search', 'create'] },
  { name: 'gitlab', transport: 'http', initialized: true, toolCount: 1, tools: ['list'] },
];

const CURSOR: ClientStatus = {
  name: 'Cursor',
  slug: 'cursor',
  detected: true,
  linked: true,
  transport: 'native SSE',
  effectiveScope: { configured: true, unscoped: false, servers: ['github'], tools: ['github__search', 'github__create'] },
};

function renderLens() {
  return render(
    <MemoryRouter>
      <AccessLens servers={SERVERS} />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  useAccessLensStore.getState().clearDraft();
  useAccessLensStore.setState({ enabled: false, isSaving: false });
  useStackStore.setState({ clients: [CURSOR], selectedNodeId: null });
});

describe('AccessLens', () => {
  it('exposes the header toggle with aria-pressed reflecting mode', () => {
    renderLens();
    const toggle = screen.getByRole('button', { name: 'Toggle Access Lens' });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(toggle);
    expect(screen.getByRole('button', { name: 'Toggle Access Lens' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('hints to select a client when none is selected', () => {
    renderLens();
    fireEvent.click(screen.getByRole('button', { name: 'Toggle Access Lens' }));
    expect(screen.getByText(/Select a client to shape its access/i)).toBeInTheDocument();
  });

  it('seeds a draft and shows the action bar once dirty', () => {
    renderLens();
    fireEvent.click(screen.getByRole('button', { name: 'Toggle Access Lens' }));

    // Select the client; the seeding effect mirrors its saved scope as baseline.
    act(() => {
      useStackStore.setState({ selectedNodeId: 'client-cursor' });
    });
    expect(screen.getByText('Cursor')).toBeInTheDocument();
    // Clean draft → no action bar yet.
    expect(screen.queryByRole('button', { name: 'Save access scope' })).not.toBeInTheDocument();

    // Grant gitlab in the draft → dirty → action bar appears with live impact.
    act(() => {
      useAccessLensStore.getState().toggleServer('gitlab');
    });
    expect(screen.getByRole('button', { name: 'Save access scope' })).toBeInTheDocument();
    // Live impact text spans multiple nodes; assert on the combined content.
    expect(document.body.textContent).toMatch(/2\s*servers granted/);
  });

  it('reverts the draft when Discard is clicked', () => {
    renderLens();
    fireEvent.click(screen.getByRole('button', { name: 'Toggle Access Lens' }));
    act(() => {
      useStackStore.setState({ selectedNodeId: 'client-cursor' });
    });
    act(() => {
      useAccessLensStore.getState().toggleServer('gitlab');
    });
    fireEvent.click(screen.getByRole('button', { name: /Discard/ }));
    expect(useAccessLensStore.getState().draft).toEqual(['github']);
  });

  it('prompts discard-with-confirm when turning the mode off while dirty', () => {
    renderLens();
    fireEvent.click(screen.getByRole('button', { name: 'Toggle Access Lens' }));
    act(() => {
      useStackStore.setState({ selectedNodeId: 'client-cursor' });
    });
    act(() => {
      useAccessLensStore.getState().toggleServer('gitlab');
    });
    // Click the toggle to turn off → confirm appears, mode still on until resolved.
    fireEvent.click(screen.getByRole('button', { name: 'Toggle Access Lens' }));
    expect(screen.getByText(/discard unsaved access changes/i)).toBeInTheDocument();
    expect(useAccessLensStore.getState().enabled).toBe(true);

    // Discard → mode off, draft cleared.
    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(useAccessLensStore.getState().enabled).toBe(false);
    expect(useAccessLensStore.getState().clientSlug).toBeNull();
  });
});
