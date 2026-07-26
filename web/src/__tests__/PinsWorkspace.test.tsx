import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { useEffect } from 'react';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import { PinsWorkspace } from '../components/workspaces/PinsWorkspace';
import { usePinsStore } from '../stores/usePinsStore';
import { useUIStore } from '../stores/useUIStore';
import * as api from '../lib/api';
import type { PinsDiff, ServerPins } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn(), copyWithToast: vi.fn() }));
vi.mock('../lib/download', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/download')>()),
  downloadTextFile: vi.fn(),
}));

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

// Captures the registry context so tests can inspect registered commands.
let registryProbe: ReturnType<typeof useCommandRegistry> | null = null;
function RegistryProbe() {
  const registry = useCommandRegistry();
  useEffect(() => {
    registryProbe = registry;
  });
  return null;
}

function renderWorkspace(initialEntry = '/pins') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <CommandRegistryProvider>
        <PinsWorkspace />
        <RegistryProbe />
      </CommandRegistryProvider>
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
  useUIStore.setState({ pinsPrefs: { attentionOnly: null, findingsOnly: false } });
  vi.restoreAllMocks();
});

describe('PinsWorkspace', () => {
  it('selects the drifted server first and renders its diff', async () => {
    renderWorkspace();

    // Drift sorts first, so zapier is the default selection and its diff loads.
    await waitFor(() => expect(api.fetchPinsDiff).toHaveBeenCalledWith('zapier'));

    expect(await screen.findByText('poisoned_tool')).toBeInTheDocument();
    // Word-level diff: only the changed token is marked, the shared word is
    // plain prose on both rows.
    expect(screen.getByRole('deletion')).toHaveTextContent('original');
    expect(screen.getByRole('insertion')).toHaveTextContent('changed');
    expect(screen.getAllByText('description').length).toBeGreaterThan(0);
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

    // The bidi override renders as a visible escape inside its word token.
    expect(await screen.findByText(/visible\\u202ehidden/)).toBeInTheDocument();
  });

  it('renders a schema panel for a schema-only drift instead of identical prose rows', async () => {
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          name: 'searcher',
          old_hash: 'h2:947cd68fbf83c18ca75435e6730174418b91fd0e',
          new_hash: 'h2:267032e068c7ee40310b8cea8e12f1248a974166',
          old_description: 'same prose',
          new_description: 'same prose',
          old_input_schema: '{"required":["query"]}',
          new_input_schema: '{"required":["query","token"]}',
          old_output_schema: '{"properties":{"ok":{"type":"boolean"}}}',
          new_output_schema: '{"properties":{"ok":{"type":"string"}}}',
          change_kinds: ['input_schema', 'output_schema'],
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    expect(await screen.findByText('input schema')).toBeInTheDocument();
    expect(screen.getByText('output schema')).toBeInTheDocument();
    expect(screen.getByText('Input schema changed')).toBeInTheDocument();
    expect(screen.getByText('Output schema changed')).toBeInTheDocument();
    expect(screen.getByText(/description unchanged/)).toBeInTheDocument();
    // The identical prose must not render as an old/new pair.
    expect(screen.queryAllByText('same prose')).toHaveLength(0);
    // The schema deltas themselves are visible without any click.
    expect(screen.getByText(/"token"/)).toBeInTheDocument();
    expect(screen.getByText(/"string"/)).toBeInTheDocument();
  });

  it('explains an uncaptured old schema and shows the new one', async () => {
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          name: 'searcher',
          old_hash: '947cd68fbf83c18ca75435e6730174418b91fd0e',
          new_hash: 'h2:267032e068c7ee40310b8cea8e12f1248a974166',
          old_description: 'same prose',
          new_description: 'same prose',
          new_input_schema: '{"required":["query"]}',
          new_output_schema: '{}',
          change_kinds: ['schema_uncaptured'],
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    expect(await screen.findByText(/pinned before schema capture/i)).toBeInTheDocument();
    expect(screen.getByText('New input schema')).toBeInTheDocument();
    expect(screen.getByText(/"query"/)).toBeInTheDocument();
  });

  it('renders the groups-rewriting advisory on modified tool cards', async () => {
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          ...zapierDiff.modified_tools[0],
          groups_rewriting: ['deploy-tools', 'ops'],
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    expect(await screen.findByText(/also rewritten by groups/i)).toBeInTheDocument();
    expect(screen.getByText('deploy-tools')).toBeInTheDocument();
    expect(screen.getByText('ops')).toBeInTheDocument();
  });

  it('opens the full-width schema diff overlay from the panel and closes on Escape', async () => {
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          name: 'searcher',
          old_hash: 'h2:947cd68fbf83c18ca75435e6730174418b91fd0e',
          new_hash: 'h2:267032e068c7ee40310b8cea8e12f1248a974166',
          old_description: 'same prose',
          new_description: 'same prose',
          old_input_schema: '{"required":["query"]}',
          new_input_schema: '{"required":["query","token"]}',
          change_kinds: ['input_schema'],
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    fireEvent.click(
      await screen.findByRole('button', { name: /expand input schema changed for searcher/i }),
    );

    const dialog = screen.getByRole('dialog', { name: /searcher - input schema changed/i });
    expect(dialog).toBeInTheDocument();
    // Rich diff: count chips and both line-number gutters. The trailing
    // comma line changes too, so: - "query" / + "query", / + "token".
    expect(dialog).toHaveTextContent('2 added');
    expect(dialog).toHaveTextContent('1 removed');
    expect(screen.getByRole('columnheader', { name: 'Old' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'New' })).toBeInTheDocument();

    fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('opens a single-pane viewer for an uncaptured schema', async () => {
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          name: 'searcher',
          old_hash: '947cd68fbf83c18ca75435e6730174418b91fd0e',
          new_hash: 'h2:267032e068c7ee40310b8cea8e12f1248a974166',
          old_description: 'same prose',
          new_description: 'same prose',
          new_input_schema: '{"required":["query"]}',
          new_output_schema: '{}',
          change_kinds: ['schema_uncaptured'],
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    fireEvent.click(
      await screen.findByRole('button', { name: /expand new input schema for searcher/i }),
    );

    const dialog = screen.getByRole('dialog', { name: /searcher - new input schema/i });
    expect(dialog).toBeInTheDocument();
    // No fake comparison against an empty document: single New gutter, no
    // Old column, and the uncaptured note in the header.
    expect(screen.queryByRole('columnheader', { name: 'Old' })).not.toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: 'New' })).toBeInTheDocument();
    expect(screen.getAllByText(/pinned before schema capture/i).length).toBeGreaterThan(0);
  });

  it('renders a multi-line uncaptured schema without literal escape sequences', async () => {
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          name: 'searcher',
          old_hash: '947cd68fbf83c18ca75435e6730174418b91fd0e',
          new_hash: 'h2:267032e068c7ee40310b8cea8e12f1248a974166',
          old_description: 'same prose',
          new_description: 'same prose',
          new_input_schema: '{"properties":{"q":{"type":"string"}},"required":["q"]}',
          new_output_schema: '{}',
          change_kinds: ['schema_uncaptured'],
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    // Pretty-printed lines render as lines, not one blob of literal \n text.
    expect(await screen.findByText(/"required": \[/)).toBeInTheDocument();
    expect(screen.queryByText((t) => t.includes('\\n'))).not.toBeInTheDocument();
  });

  it('hides clean servers behind the attention filter by default', async () => {
    renderWorkspace();

    // zapier drifts, so the attention default is on and clean github hides.
    expect(await screen.findByRole('button', { name: /^zapier/ })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^github/ })).not.toBeInTheDocument();

    const toggle = screen.getByRole('button', { name: /attention/i });
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(toggle);

    expect(screen.getByRole('button', { name: /^github/ })).toBeInTheDocument();
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    // The explicit choice persists.
    expect(useUIStore.getState().pinsPrefs.attentionOnly).toBe(false);
    // The tally never shrinks to the filtered view.
    expect(screen.getByText('2 servers pinned · 1 drifted')).toBeInTheDocument();
  });

  it('keeps a deep-linked clean server visible outside the attention filter', async () => {
    renderWorkspace('/pins?server=github');

    // github is clean and outside the attention filter, but the deep link
    // keeps it selectable instead of silently reselecting a drifted server.
    expect(await screen.findByText('create_issue')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^github/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^zapier/ })).toBeInTheDocument();
  });

  it('shows rail density: tool count and last-verified age', async () => {
    renderWorkspace();

    expect(await screen.findByText(/0 tools · verified/)).toBeInTheDocument();
  });

  it('scrolls the drift section into view for view=drift', async () => {
    const scrollSpy = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;
    try {
      renderWorkspace('/pins?server=zapier&view=drift');

      await waitFor(() => expect(scrollSpy).toHaveBeenCalled());
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it('drops the one-shot view param when another server is selected', async () => {
    const scrollSpy = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;
    try {
      renderWorkspace('/pins?server=zapier&view=drift');
      await waitFor(() => expect(scrollSpy).toHaveBeenCalled());
      scrollSpy.mockClear();

      // Selecting a server is a new intent; the stale scroll target must not
      // keep re-firing on every rail move. github is outside the attention
      // filter, so surface it first.
      fireEvent.click(screen.getByRole('button', { name: /attention/i }));
      fireEvent.click(screen.getByRole('button', { name: /^github/ }));

      await screen.findByText('create_issue');
      expect(scrollSpy).not.toHaveBeenCalled();
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it('activates Approve with Enter from the rail', async () => {
    const approve = vi.spyOn(api, 'approveServerPins').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchServerPins').mockResolvedValue({
      github: serverPins('pinned'),
      zapier: serverPins('pinned'),
    });

    renderWorkspace();
    await screen.findByText('poisoned_tool');

    // First Enter moves focus to Approve; useListNav preventDefaults the
    // keydown, so the second Enter must still activate it.
    fireEvent.keyDown(document, { key: 'Enter' });
    const approveButton = screen.getByRole('button', { name: /approve/i });
    expect(approveButton).toHaveFocus();
    fireEvent.keyDown(document, { key: 'Enter' });

    await waitFor(() =>
      expect(approve).toHaveBeenCalledWith('zapier', 'h2:reviewed-live-fingerprint'),
    );
  });

  it('pins the automatic selection into the URL so it survives filter changes', async () => {
    renderWorkspace();

    // zapier was auto-selected (drift-first); the selection is written to
    // the URL so a poll that clears its drift cannot silently switch the
    // review target.
    await screen.findByText('poisoned_tool');
    expect(screen.getByRole('button', { name: /^zapier/ })).toHaveAttribute('aria-current', 'true');
  });

  it('marks servers with findings in the rail and header tally', async () => {
    usePinsStore.setState({
      pins: {
        github: serverPins('pinned', {
          create_issue: {
            hash: 'h2:aaaa11112222333344445555',
            name: 'create_issue',
            description: 'Create an issue',
            pinned_at: '2026-07-01T00:00:00Z',
            findings: [
              {
                code: 'P001',
                severity: 'warn',
                confidence: 'high',
                field: 'description',
                message: 'hidden-instruction phrasing',
              },
              {
                code: 'P004',
                severity: 'info',
                confidence: 'low',
                field: 'description',
                message: 'emphasis words',
              },
            ],
          },
        }),
        zapier: serverPins('drift'),
      },
    });

    renderWorkspace();

    // Header tally: 2 pinned, 1 drifted, 1 with findings.
    expect(await screen.findByText('2 servers pinned · 1 drifted · 1 with findings')).toBeInTheDocument();
    // Rail mark counts warn+critical only (the info finding is excluded).
    expect(screen.getByLabelText('1 finding on github')).toBeInTheDocument();
  });

  it('approves a small clean drift in one click but gates a large drift behind a confirmation', async () => {
    const approve = vi.spyOn(api, 'approveServerPins').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchServerPins').mockResolvedValue({
      github: serverPins('pinned'),
      zapier: serverPins('pinned'),
    });
    // Six changes: five modified tools plus one removed.
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: Array.from({ length: 5 }, (_, i) => ({
        ...zapierDiff.modified_tools[0],
        name: `tool_${i}`,
      })),
      new_tools: [],
      removed_tools: ['retired_tool'],
    });

    renderWorkspace();

    const approveButton = await screen.findByRole('button', { name: /approve 6 changes/i });
    fireEvent.click(approveButton);

    // Gated: nothing approved yet, the confirmation restates the decision.
    expect(approve).not.toHaveBeenCalled();
    const dialog = screen.getByRole('alertdialog', { name: /approve 6 changes on zapier/i });
    expect(dialog).toHaveTextContent('5 modified');
    expect(dialog).toHaveTextContent('1 removed');

    fireEvent.click(within(dialog).getByRole('button', { name: /approve 6 changes/i }));
    await waitFor(() =>
      expect(approve).toHaveBeenCalledWith('zapier', 'h2:reviewed-live-fingerprint'),
    );
  });

  it('gates approve behind a confirmation when the drift carries warn findings', async () => {
    const approve = vi.spyOn(api, 'approveServerPins').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchPinsDiff').mockResolvedValue({
      ...zapierDiff,
      modified_tools: [
        {
          ...zapierDiff.modified_tools[0],
          findings: [
            {
              code: 'P001',
              severity: 'warn',
              confidence: 'high',
              field: 'description',
              message: 'hidden-instruction phrasing',
            },
          ],
        },
      ],
      new_tools: [],
      removed_tools: [],
    });

    renderWorkspace();

    fireEvent.click(await screen.findByRole('button', { name: /approve 1 change/i }));

    expect(approve).not.toHaveBeenCalled();
    const dialog = screen.getByRole('alertdialog', { name: /approve 1 change on zapier/i });
    expect(dialog).toHaveTextContent('1 warn-or-critical scan finding');
  });

  it('resets pins behind a danger confirmation and refreshes the workspace', async () => {
    const reset = vi.spyOn(api, 'resetServerPins').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchServerPins').mockResolvedValue({ github: serverPins('pinned') });

    renderWorkspace('/pins?server=zapier');
    await screen.findByText('poisoned_tool');

    fireEvent.click(screen.getByRole('button', { name: /reset pins for zapier/i }));

    const dialog = screen.getByRole('alertdialog', { name: /reset pins/i });
    expect(dialog).toHaveTextContent('zapier');
    expect(dialog).toHaveTextContent(/re-pins from scratch/i);
    expect(reset).not.toHaveBeenCalled();

    fireEvent.click(within(dialog).getByRole('button', { name: /reset pins for zapier/i }));

    await waitFor(() => expect(reset).toHaveBeenCalledWith('zapier'));
    // The workspace reflects the removal without a manual refresh.
    await waitFor(() =>
      expect(screen.queryByRole('button', { name: /^zapier/ })).not.toBeInTheDocument(),
    );
  });

  it('exports the loaded diff as JSON', async () => {
    const download = vi.mocked((await import('../lib/download')).downloadTextFile);

    renderWorkspace();
    await screen.findByText('poisoned_tool');

    fireEvent.click(screen.getByRole('button', { name: /export diff/i }));

    expect(download).toHaveBeenCalledTimes(1);
    const [content, filename, mime] = download.mock.calls[0];
    expect(filename).toContain('gridctl-pins-diff-zapier');
    expect(mime).toBe('application/json');
    const parsed = JSON.parse(content);
    expect(parsed.server).toBe('zapier');
    expect(parsed.live_server_hash).toBe('h2:reviewed-live-fingerprint');
    expect(parsed.modified_tools).toHaveLength(1);
    expect(typeof parsed.exported_at).toBe('string');
  });

  it('copies the full live server hash for the CLI handoff', async () => {
    const { copyWithToast } = await import('../components/ui/Toast');

    renderWorkspace();
    await screen.findByText('poisoned_tool');

    fireEvent.click(screen.getByRole('button', { name: /copy live server hash/i }));

    expect(vi.mocked(copyWithToast)).toHaveBeenCalledWith(
      'h2:reviewed-live-fingerprint',
      'Live server hash',
    );
  });

  it('collapses table findings behind the badge and filters to flagged tools', async () => {
    usePinsStore.setState({
      pins: {
        github: serverPins('pinned', {
          create_issue: {
            hash: 'h2:aaaa11112222333344445555',
            name: 'create_issue',
            description: 'Create an issue',
            pinned_at: '2026-07-01T00:00:00Z',
            findings: [
              {
                code: 'P001',
                severity: 'warn',
                confidence: 'high',
                field: 'description',
                message: 'hidden-instruction phrasing',
              },
            ],
          },
          merge_pr: {
            hash: 'h2:bbbb11112222333344445555',
            name: 'merge_pr',
            description: 'Merge a pull request',
            pinned_at: '2026-07-01T00:00:00Z',
          },
        }),
      },
    });

    renderWorkspace('/pins?server=github');
    await screen.findByText('create_issue');

    // Collapsed by default: the badge is visible, the finding body is not.
    expect(screen.queryByText('hidden-instruction phrasing')).not.toBeInTheDocument();
    const badgeToggle = screen.getByTitle('Show findings');
    expect(badgeToggle).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(badgeToggle);
    expect(screen.getByText('hidden-instruction phrasing')).toBeInTheDocument();

    // Findings-only filter hides the clean row and shows the count.
    fireEvent.click(screen.getByRole('button', { name: /findings only/i }));
    expect(screen.getByText('1 of 2 tools')).toBeInTheDocument();
    expect(screen.queryByText('merge_pr')).not.toBeInTheDocument();
    expect(useUIStore.getState().pinsPrefs.findingsOnly).toBe(true);
  });

  it('activates the findings filter when landing on view=findings', async () => {
    usePinsStore.setState({
      pins: {
        github: serverPins('pinned', {
          create_issue: {
            hash: 'h2:aaaa11112222333344445555',
            name: 'create_issue',
            description: 'Create an issue',
            pinned_at: '2026-07-01T00:00:00Z',
            findings: [
              {
                code: 'P001',
                severity: 'warn',
                confidence: 'high',
                field: 'description',
                message: 'msg',
              },
            ],
          },
          merge_pr: {
            hash: 'h2:bbbb11112222333344445555',
            name: 'merge_pr',
            description: 'Merge a pull request',
            pinned_at: '2026-07-01T00:00:00Z',
          },
        }),
      },
    });

    renderWorkspace('/pins?server=github&view=findings');

    expect(await screen.findByText('create_issue')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /findings only/i })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.queryByText('merge_pr')).not.toBeInTheDocument();
    // A findings deep link lands with the finding bodies already expanded.
    expect(screen.getByText('msg')).toBeInTheDocument();
  });

  it('registers pins palette commands scoped to the workspace', async () => {
    renderWorkspace();
    await screen.findByText('poisoned_tool');

    const ids = registryProbe!.getSortedCommands('', 'pins').map((c) => c.id);
    expect(ids).toEqual(
      expect.arrayContaining([
        'pins:approve',
        'pins:reverify',
        'pins:export-diff',
        'pins:copy-live-hash',
        'pins:toggle-attention',
        'pins:toggle-findings',
        'pins:reset',
      ]),
    );
  });
});
