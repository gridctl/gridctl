import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { PinsWorkspace } from '../components/workspaces/PinsWorkspace';
import { usePinsStore } from '../stores/usePinsStore';
import * as api from '../lib/api';
import type { PinsDiff, ServerPins } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function serverPins(status: ServerPins['status'], tools: ServerPins['tools'] = {}): ServerPins {
  return {
    server_hash: 'h2:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    tool_count: Object.keys(tools).length,
    status,
    tools,
  };
}

const zapierDiff: PinsDiff = {
  server: 'zapier',
  status: 'drift',
  live_server_hash: 'h2:reviewed-live-fingerprint',
  modified_tools: [
    {
      name: 'poisoned_tool',
      old_hash: 'h2:947cd68fbf83c18ca75435e6730174418b91fd0e',
      new_hash: 'h2:267032e068c7ee40310b8cea8e12f1248a974166',
      old_description: 'original description',
      new_description: 'changed description',
    },
  ],
  new_tools: ['brand_new_tool'],
  removed_tools: ['retired_tool'],
};

function renderWorkspace(initialEntry = '/pins') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <PinsWorkspace />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  usePinsStore.setState({
    pins: {
      github: serverPins('pinned', {
        create_issue: {
          hash: 'h2:aaaa11112222333344445555',
          name: 'create_issue',
          description: 'Create an issue',
          pinned_at: '2026-07-01T00:00:00Z',
        },
      }),
      zapier: serverPins('drift'),
    },
  });
  vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue(zapierDiff);
});

afterEach(() => {
  usePinsStore.setState({ pins: null });
  vi.restoreAllMocks();
});

describe('PinsWorkspace', () => {
  it('selects the drifted server first and renders its diff', async () => {
    renderWorkspace();

    // Drift sorts first, so zapier is the default selection and its diff loads.
    await waitFor(() => expect(api.fetchPinsDiff).toHaveBeenCalledWith('zapier'));

    expect(await screen.findByText('poisoned_tool')).toBeInTheDocument();
    expect(screen.getByText(/original description/)).toBeInTheDocument();
    expect(screen.getByText(/changed description/)).toBeInTheDocument();
    expect(screen.getByText(/h2:947cd68fbf83/)).toBeInTheDocument();
    expect(screen.getByText(/h2:267032e068c7/)).toBeInTheDocument();
    expect(screen.getByText('brand_new_tool')).toBeInTheDocument();
    expect(screen.getByText('retired_tool')).toBeInTheDocument();
  });

  it('co-locates Approve with the rendered diff and approves the reviewed server', async () => {
    const approve = vi.spyOn(api, 'approveServerPins').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchServerPins').mockResolvedValue({
      github: serverPins('pinned'),
      zapier: serverPins('pinned'),
    });

    renderWorkspace();

    // Approve is disabled until the diff has rendered — no blind approval.
    const approveButton = await screen.findByRole('button', { name: /approve/i });
    await screen.findByText('poisoned_tool');

    const driftSection = screen.getByRole('region', { name: /schema drift for zapier/i });
    expect(driftSection).toContainElement(approveButton);
    expect(approveButton).toHaveTextContent(/3 changes/);

    fireEvent.click(approveButton);
    // The approval is bound to the reviewed diff's fingerprint so definitions
    // that change after review are rejected server-side.
    await waitFor(() =>
      expect(approve).toHaveBeenCalledWith('zapier', 'h2:reviewed-live-fingerprint'),
    );
  });

  it('honors ?server= selection and shows pinned tool records', async () => {
    renderWorkspace('/pins?server=github');

    expect(await screen.findByText('create_issue')).toBeInTheDocument();
    expect(screen.getByText(/h2:aaaa11112222/)).toBeInTheDocument();
    // A pinned server has no drift section.
    expect(screen.queryByText(/schema drift/i)).not.toBeInTheDocument();
  });

  it('renders hidden characters in descriptions as visible escapes', async () => {
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          ...zapierDiff.modified_tools[0],
          new_description: 'visible‮hidden payload',
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    expect(await screen.findByText(/visible\\u202ehidden payload/)).toBeInTheDocument();
  });
});
