import { describe, expect, it } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { Badges } from '../components/connections/ConnectionsRail';
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
    render(<Badges client={drifted} />);
    expect(screen.getByText('Drifted').className).toContain('status-pending');
  });
});
