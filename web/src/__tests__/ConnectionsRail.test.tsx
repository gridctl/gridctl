import { describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { fireEvent, render, screen } from '@testing-library/react';
import { ClientBadgeList, ConnectionsRail } from '../components/connections/ConnectionsRail';
import type { ClientStatus } from '../types';

const drifted: ClientStatus = {
  name: 'Claude',
  slug: 'claude',
  detected: true,
  linked: true,
  transport: 'http',
  drifted: true,
};

describe('ConnectionsRail badges', () => {
  it('renders Drifted with the pending status token', () => {
    render(<ClientBadgeList client={drifted} />);
    expect(screen.getByText('Drifted').className).toContain('status-pending');
  });
});

describe('ConnectionsRail connect toggle', () => {
  it('does not select the row when the toggle is clicked', () => {
    const onSelect = vi.fn();
    const onToggle = vi.fn();
    const client: ClientStatus = {
      name: 'Claude',
      slug: 'claude',
      detected: true,
      linked: false,
      transport: 'http',
    };
    render(
      <ConnectionsRail
        clients={[client]}
        totalCount={1}
        activeSlug={null}
        onSelect={onSelect}
        healthOf={() => ({ attention: false, reasons: [] })}
        hasLiveActivity={() => false}
        desiredOf={() => false}
        onToggle={onToggle}
        attentionOnly={false}
        onToggleAttention={() => {}}
      />,
    );

    fireEvent.click(screen.getByRole('switch', { name: 'Link Claude' }));

    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(onSelect).not.toHaveBeenCalled();
  });
});
