import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { ToolDetailPanel } from '../components/workspaces/ToolDetailPanel';
import { prefKey } from '../lib/preferences';
import type { ToolRow } from '../hooks/useToolsEditor';
import type { ServerPins } from '../lib/api';

// Mirrors copyWithToast over the real clipboard helper so the toast outcome
// is observable through the mocked showToast.
vi.mock('../components/ui/Toast', async () => {
  const { copyTextToClipboard } = await import('../lib/clipboard');
  const showToast = vi.fn();
  return {
    showToast,
    copyWithToast: async (text: string, label: string) => {
      try {
        await copyTextToClipboard(text);
        showToast('success', `${label} copied`);
      } catch {
        showToast('error', 'Copy failed');
      }
    },
  };
});
import { showToast } from '../components/ui/Toast';

const TOOL: ToolRow = {
  name: 'getThing',
  description: 'Fetch a thing by id',
} as ToolRow;

const SCHEMA = { type: 'object', properties: { id: { type: 'string' } } };

function renderPanel(overrides = {}) {
  return render(
    <ToolDetailPanel
      serverName="atlassian"
      tool={TOOL}
      schema={SCHEMA}
      enabled
      auditMode={false}
      auditState={null}
      onClose={() => {}}
      {...overrides}
    />,
  );
}

beforeEach(() => {
  localStorage.clear();
  vi.mocked(showToast).mockClear();
});

describe('ToolDetailPanel', () => {
  it('shows an empty state when no tool is selected', () => {
    render(
      <ToolDetailPanel
        serverName="atlassian"
        tool={null}
        enabled={false}
        auditMode={false}
        auditState={null}
        onClose={() => {}}
      />,
    );
    expect(screen.getByText(/select a tool to view/i)).toBeInTheDocument();
    // No control without a tool.
    expect(screen.queryByTitle(/increase font size/i)).not.toBeInTheDocument();
  });

  it('renders the description, schema, and the text-size control', () => {
    renderPanel();
    expect(screen.getByText('Fetch a thing by id')).toBeInTheDocument();
    expect(screen.getByTitle(/increase font size/i)).toBeInTheDocument();
    // Default reflects the pane default (12px).
    expect(screen.getByText('12px')).toBeInTheDocument();
  });

  it('scales the displayed size when zooming in', () => {
    renderPanel();
    fireEvent.click(screen.getByTitle(/increase font size/i));
    expect(screen.getByText('13px')).toBeInTheDocument();
    expect(localStorage.getItem(prefKey('tools-zoom'))).toBe('13');
  });

  it('shows the Usage section with calls and tokens outside audit mode', () => {
    renderPanel({
      usage: { calls: 12, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 1200, outputTokens: 400 },
    });
    expect(screen.getByText('Usage')).toBeInTheDocument();
    expect(screen.getByText('Calls')).toBeInTheDocument();
    expect(screen.getByText('12')).toBeInTheDocument();
    expect(screen.getByText('1.2k')).toBeInTheDocument();
    expect(screen.getByText('400')).toBeInTheDocument();
    // The dollar-cost layer is gone: no cost stat renders.
    expect(screen.queryByText(/Cost/)).not.toBeInTheDocument();
  });

  it('hides the Usage section without usage data or audit state', () => {
    renderPanel();
    expect(screen.queryByText('Usage')).not.toBeInTheDocument();
  });
});

describe('ToolDetailPanel — Hints', () => {
  it('lists declared annotations with the unverified-claim caption', () => {
    renderPanel({
      tool: {
        name: 'getThing',
        description: 'Fetch a thing by id',
        annotations: { readOnlyHint: true, openWorldHint: false },
      } as ToolRow,
    });

    expect(screen.getByText('Hints')).toBeInTheDocument();
    expect(screen.getByText('RO')).toBeInTheDocument();
    expect(screen.getByText(/does not modify state/i)).toBeInTheDocument();
    expect(screen.getByText('CLOSED')).toBeInTheDocument();
    expect(screen.getByText(/reported by the server, not verified by gridctl/i)).toBeInTheDocument();
  });

  it('states the pessimistic spec default for unannotated tools', () => {
    renderPanel();
    expect(screen.getByText(/no annotations declared/i)).toBeInTheDocument();
    expect(
      screen.getByText(/treated as potentially destructive and open-world/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/reported by the server/i)).not.toBeInTheDocument();
  });
});

describe('ToolDetailPanel — description clamp', () => {
  it('clamps a 2,000-character description and discloses the hidden count', () => {
    const long = 'word '.repeat(400);
    renderPanel({ tool: { name: 'getThing', description: long } as ToolRow });
    const desc = document.querySelector('p.whitespace-pre-wrap') as HTMLElement;
    const shown = desc.textContent ?? '';
    expect(shown.endsWith('…')).toBe(true);
    const visible = shown.slice(0, -1);
    expect(visible.length).toBeLessThanOrEqual(480);
    const hidden = long.length - visible.length;
    const button = screen.getByRole('button', {
      name: `Show more (${hidden.toLocaleString('en-US')} more characters)`,
    });
    expect(button).toHaveAttribute('aria-expanded', 'false');
    expect(button).toHaveAttribute('aria-controls', desc.id);

    fireEvent.click(button);
    expect(desc.textContent).toBe(long);
    expect(screen.getByRole('button', { name: 'Show less' })).toHaveAttribute('aria-expanded', 'true');
  });

  it('shows a short description with no toggle', () => {
    renderPanel();
    expect(screen.queryByRole('button', { name: /show more/i })).not.toBeInTheDocument();
  });

  it('returns to collapsed when another tool is selected', () => {
    const long = 'word '.repeat(400);
    const { rerender } = renderPanel({ tool: { name: 'a', description: long } as ToolRow });
    fireEvent.click(screen.getByRole('button', { name: /show more/i }));
    expect(screen.getByRole('button', { name: 'Show less' })).toBeInTheDocument();
    rerender(
      <ToolDetailPanel
        serverName="atlassian"
        tool={{ name: 'b', description: long } as ToolRow}
        schema={SCHEMA}
        enabled
        auditMode={false}
        auditState={null}
        onClose={() => {}}
      />,
    );
    expect(screen.getByRole('button', { name: /show more/i })).toHaveAttribute('aria-expanded', 'false');
  });

  it('renders markup and markdown literally', () => {
    const text = 'Use <b>x</b> and [a](http://x) carefully';
    renderPanel({ tool: { name: 'getThing', description: text } as ToolRow });
    const desc = screen.getByText(text);
    expect(desc.querySelector('b')).toBeNull();
    expect(desc.querySelector('a')).toBeNull();
  });
});

describe('ToolDetailPanel — parameters and raw schema', () => {
  const PARAM_SCHEMA = {
    type: 'object',
    properties: {
      id: { type: 'string', description: 'The thing id' },
      limit: { type: 'integer', default: 10, enum: [10, 50] },
    },
    required: ['id'],
  };

  it('renders a parameters table with required, default, and enum values', () => {
    renderPanel({ schema: PARAM_SCHEMA });
    const table = screen.getByRole('table', { name: 'getThing parameters' });
    const headers = within(table).getAllByRole('columnheader').map((h) => h.textContent);
    expect(headers).toEqual(['Parameter', 'Type', 'Description']);
    const rows = within(table).getAllByRole('row').slice(1);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent('id');
    expect(rows[0]).toHaveTextContent('string');
    expect(within(rows[0]).getByText('required')).toBeInTheDocument();
    expect(rows[0]).toHaveTextContent('The thing id');
    expect(rows[1]).toHaveTextContent('limit');
    expect(rows[1]).toHaveTextContent('integer');
    expect(within(rows[1]).queryByText('required')).not.toBeInTheDocument();
    expect(within(rows[1]).getByText('Default: 10')).toBeInTheDocument();
    expect(within(rows[1]).getByText('One of: 10, 50')).toBeInTheDocument();
  });

  it('caps enum values at eight and truncates long defaults', () => {
    renderPanel({
      schema: {
        properties: {
          p: { type: 'integer', enum: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10], default: 'x'.repeat(100) },
        },
      },
    });
    expect(screen.getByText('One of: 1, 2, 3, 4, 5, 6, 7, 8 +2 more')).toBeInTheDocument();
    expect(screen.getByText(`Default: "${'x'.repeat(79)}…`)).toBeInTheDocument();
  });

  it('keeps the raw schema collapsed when the table has rows', () => {
    renderPanel({ schema: PARAM_SCHEMA });
    const toggle = screen.getByRole('button', { name: 'Raw schema' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByLabelText('getThing input schema')).not.toBeInTheDocument();
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByLabelText('getThing input schema')).toBeInTheDocument();
    expect(document.getElementById(toggle.getAttribute('aria-controls')!)).not.toBeNull();
  });

  it('opens the raw schema for a schema that cannot be summarized', () => {
    renderPanel({ schema: { type: 'string' } });
    expect(screen.getByText('Parameters cannot be summarized; see the raw schema.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Raw schema' })).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByLabelText('getThing input schema')).toBeInTheDocument();
  });

  it('shows No parameters. for an empty schema with the raw schema collapsed', () => {
    renderPanel({ schema: {} });
    expect(screen.getByText('No parameters.')).toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Raw schema' })).toHaveAttribute('aria-expanded', 'false');
  });

  it('renders notes under the table', () => {
    renderPanel({ schema: { ...PARAM_SCHEMA, additionalProperties: true } });
    expect(screen.getByText(/accepts additional properties/i)).toBeInTheDocument();
  });

  it('keeps the user raw-schema choice across tool selections', () => {
    const { rerender } = renderPanel({ schema: PARAM_SCHEMA });
    fireEvent.click(screen.getByRole('button', { name: 'Raw schema' }));
    rerender(
      <ToolDetailPanel
        serverName="atlassian"
        tool={{ name: 'other' } as ToolRow}
        schema={PARAM_SCHEMA}
        enabled
        auditMode={false}
        auditState={null}
        onClose={() => {}}
      />,
    );
    expect(screen.getByLabelText('other input schema')).toBeInTheDocument();
  });

  it('renders neither table nor disclosure without a schema', () => {
    renderPanel({ schema: undefined });
    expect(screen.getByText('No input schema available.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Raw schema' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Copy input schema' })).not.toBeInTheDocument();
  });

  it('does not throw on a malformed schema', () => {
    renderPanel({ schema: { properties: { a: true, b: 5 }, required: 'a' } as Record<string, unknown> });
    const table = screen.getByRole('table', { name: 'getThing parameters' });
    expect(within(table).getAllByText('any')).toHaveLength(2);
  });
});

describe('ToolDetailPanel — copy', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('copies the pretty-printed schema while collapsed', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderPanel();
    expect(screen.getByRole('button', { name: 'Raw schema' })).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(screen.getByRole('button', { name: 'Copy input schema' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(JSON.stringify(SCHEMA, null, 2)));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Input schema copied'));
  });

  it('copies the prefixed name', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'Copy prefixed name atlassian__getThing' }));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith('atlassian__getThing'));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Tool name copied'));
  });

  it('reports a clipboard failure', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('denied'));
    Object.assign(navigator, { clipboard: { writeText } });
    const exec = document.execCommand;
    document.execCommand = vi.fn().mockReturnValue(false);
    try {
      renderPanel();
      fireEvent.click(screen.getByRole('button', { name: /copy prefixed name/i }));
      await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', 'Copy failed'));
    } finally {
      document.execCommand = exec;
    }
  });
});

describe('ToolDetailPanel — links', () => {
  it('builds Traces and Metrics hrefs', () => {
    renderPanel({ usage: { calls: 1, lastCalledAt: '2026-07-01T00:00:00Z' } });
    expect(screen.getByRole('link', { name: 'Traces' })).toHaveAttribute(
      'href',
      '/traces?q=getThing&server=atlassian&errors=0',
    );
    expect(screen.getByRole('link', { name: 'Metrics' })).toHaveAttribute(
      'href',
      '/metrics?scope=tools&selected=atlassian__getThing',
    );
  });

  it('omits Metrics without usage', () => {
    renderPanel();
    expect(screen.getByRole('link', { name: 'Traces' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Metrics' })).not.toBeInTheDocument();
  });

  it('encodes names', () => {
    renderPanel({
      serverName: 'a b&c',
      tool: { name: 'x/y?z' } as ToolRow,
      usage: { calls: 1 },
    });
    expect(screen.getByRole('link', { name: 'Traces' })).toHaveAttribute(
      'href',
      '/traces?q=x%2Fy%3Fz&server=a%20b%26c&errors=0',
    );
    expect(screen.getByRole('link', { name: 'Metrics' })).toHaveAttribute(
      'href',
      '/metrics?scope=tools&selected=a%20b%26c__x%2Fy%3Fz',
    );
  });

  it('hands a plain click to onNavigate and leaves modified clicks alone', () => {
    const onNavigate = vi.fn();
    renderPanel({ onNavigate });
    const link = screen.getByRole('link', { name: 'Traces' });

    const plain = fireEvent.click(link);
    expect(plain).toBe(false); // default prevented
    expect(onNavigate).toHaveBeenCalledWith('/traces?q=getThing&server=atlassian&errors=0');

    onNavigate.mockClear();
    expect(fireEvent.click(link, { metaKey: true })).toBe(true);
    expect(fireEvent.click(link, { ctrlKey: true })).toBe(true);
    expect(fireEvent.click(link, { shiftKey: true })).toBe(true);
    expect(fireEvent.click(link, { altKey: true })).toBe(true);
    expect(fireEvent.click(link, { button: 1 })).toBe(true);
    expect(onNavigate).not.toHaveBeenCalled();
  });
});

describe('ToolDetailPanel — pin chip', () => {
  function pinEntry(status: ServerPins['status'], tools: string[]): ServerPins {
    return {
      server_hash: 'h',
      pinned_at: '2026-07-01T00:00:00Z',
      last_verified_at: '2026-07-01T00:00:00Z',
      tool_count: tools.length,
      status,
      tools: Object.fromEntries(tools.map((t) => [t, {} as ServerPins['tools'][string]])),
    };
  }

  it('renders nothing without a pin entry', () => {
    renderPanel();
    expect(screen.queryByRole('link', { name: /pin status/i })).not.toBeInTheDocument();
  });

  it.each([
    ['pinned', 'Pinned', '/pins?server=atlassian'],
    ['drift', 'Server drift', '/pins?server=atlassian&view=drift'],
    ['approved_pending_redeploy', 'Approved', '/pins?server=atlassian'],
  ] as const)('labels %s as %s', (status, label, href) => {
    renderPanel({ pin: pinEntry(status, ['getThing']) });
    const chip = screen.getByRole('link', { name: `Pin status: ${label}. Open in Pins` });
    expect(chip).toHaveTextContent(label);
    expect(chip).toHaveAttribute('href', href);
  });

  it('reads Not in pin set for an exposed tool missing from the pin set', () => {
    renderPanel({ pin: pinEntry('drift', ['other']) });
    const chip = screen.getByRole('link', { name: 'Pin status: Not in pin set. Open in Pins' });
    expect(chip).toHaveAttribute('href', '/pins?server=atlassian');
  });

  it('stays silent for a tool the saved allow-list hides', () => {
    renderPanel({ pin: pinEntry('pinned', ['other']), hiddenByAllowList: true });
    expect(screen.queryByRole('link', { name: /pin status/i })).not.toBeInTheDocument();
  });

  it('routes the chip through onNavigate', () => {
    const onNavigate = vi.fn();
    renderPanel({ pin: pinEntry('drift', ['getThing']), onNavigate });
    fireEvent.click(screen.getByRole('link', { name: /pin status/i }));
    expect(onNavigate).toHaveBeenCalledWith('/pins?server=atlassian&view=drift');
  });
});
