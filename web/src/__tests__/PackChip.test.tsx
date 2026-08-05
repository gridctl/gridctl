import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { PackChip } from '../components/registry/PackChip';
import { useRegistryStore } from '../stores/useRegistryStore';
import type { PackListItem } from '../lib/api';

const teamPack: PackListItem = {
  name: 'team-pack',
  origin: { source: 'team-repo', repo: 'https://github.com/acme/team-repo' },
  counts: { skills: 1, agents: 1, rules: 0, wiring: false },
  applied: true,
  needs_attention: false,
};

function renderChip(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useRegistryStore.setState({ packs: null });
});

describe('PackChip', () => {
  it('joins a source to its owning pack from the store', () => {
    useRegistryStore.setState({ packs: [teamPack] });
    renderChip(<PackChip source="team-repo" />);
    expect(screen.getByText('pack: team-pack')).toBeInTheDocument();
  });

  it('renders nothing while packs are unloaded', () => {
    renderChip(<PackChip source="team-repo" />);
    expect(screen.queryByText(/^pack:/)).not.toBeInTheDocument();
  });

  it('renders nothing when no pack owns the source', () => {
    useRegistryStore.setState({ packs: [teamPack] });
    renderChip(<PackChip source="plain-repo" />);
    expect(screen.queryByText(/^pack:/)).not.toBeInTheDocument();
  });

  it('renders directly from a wire-carried tag without any join', () => {
    renderChip(<PackChip pack="wire-pack" />);
    expect(screen.getByText('pack: wire-pack')).toBeInTheDocument();
  });

  it('stops propagation so a chip inside a clickable card never selects it', () => {
    const onCardClick = vi.fn();
    renderChip(
      <div onClick={onCardClick} role="presentation">
        <PackChip pack="wire-pack" />
      </div>,
    );
    fireEvent.click(screen.getByText('pack: wire-pack'));
    expect(onCardClick).not.toHaveBeenCalled();
  });
});
