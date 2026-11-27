import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { ToolTryIt, CANCELLED_MESSAGE } from '../components/workspaces/ToolTryIt';
import { ToolDetailPanel } from '../components/workspaces/ToolDetailPanel';
import { useToolCallHistoryStore } from '../stores/useToolCallHistoryStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useUIStore } from '../stores/useUIStore';
import type { ToolRow } from '../hooks/useToolsEditor';
import type { PolicyToolStatus } from '../lib/api';

vi.mock('../lib/clipboard', () => ({ copyTextToClipboard: vi.fn().mockResolvedValue(undefined) }));
import { copyTextToClipboard } from '../lib/clipboard';

const READ_ONLY: ToolRow = { name: 'get_page', annotations: { readOnlyHint: true } } as ToolRow;

const SCHEMA = {
  type: 'object',
  properties: {
    title: { type: 'string', description: 'Page title' },
    count: { type: 'integer', minimum: 1, default: 5 },
    mode: { type: 'string', enum: ['fast', 'slow'] },
    verbose: { type: 'boolean' },
    tags: { type: 'array', items: { type: 'string' } },
  },
  required: ['title'],
};

const OK = {
  schema_version: 1,
  client: 'ui',
  name: 'wiki__get_page',
  outcome: { disposition: 'completed', stage: 'downstream', reason: 'ok', completion: 'complete' },
  result: { content: [{ type: 'text', text: 'hello' }] },
  error: null,
};

function respond(status: number, body: unknown) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

function stubFetch(status = 200, body: unknown = OK) {
  const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(respond(status, body)));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

// A fetch that stays pending until released, so a test can act mid-call.
function pendingFetch() {
  let release: (r: Response) => void = () => {};
  const fetchMock = vi.fn().mockImplementation(
    () =>
      new Promise<Response>((resolve) => {
        release = resolve;
      }),
  );
  vi.stubGlobal('fetch', fetchMock);
  return {
    fetchMock,
    release: (r: Response) => release(r),
    signal: () => (fetchMock.mock.calls[0][1] as RequestInit).signal as AbortSignal,
  };
}

function sentBody(fetchMock: ReturnType<typeof vi.fn>, call = 0) {
  return JSON.parse((fetchMock.mock.calls[call][1] as RequestInit).body as string);
}

function renderTryIt(
  overrides: Partial<{
    tool: ToolRow;
    schema: Record<string, unknown> | undefined;
    exposed: boolean;
    draftDiffers: boolean;
    policy: PolicyToolStatus;
  }> = {},
) {
  const props = {
    serverName: 'wiki',
    tool: READ_ONLY,
    schema: SCHEMA as Record<string, unknown> | undefined,
    exposed: true,
    draftDiffers: false,
    fontSize: 12,
    ...overrides,
  };
  return render(<ToolTryIt key={`${props.serverName}__${props.tool.name}`} {...props} />);
}

function expand() {
  fireEvent.click(screen.getByRole('button', { name: 'Try it' }));
}

function runButton() {
  return screen.getByRole('button', { name: 'Run' });
}

beforeEach(() => {
  useToolCallHistoryStore.setState({ byTool: {}, tryItOpen: false });
  useAuthStore.setState({ authRequired: false });
  localStorage.clear();
  sessionStorage.clear();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.mocked(copyTextToClipboard).mockClear();
});

describe('ToolTryIt — disclosure and form', () => {
  it('starts collapsed and expands to a schema-derived form', () => {
    renderTryIt();
    const toggle = screen.getByRole('button', { name: 'Try it' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByText('Call this tool through the gateway')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Run' })).not.toBeInTheDocument();

    expand();
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const title = screen.getByLabelText(/^title/);
    expect(title).toHaveAttribute('aria-required', 'true');
    expect(screen.getByLabelText(/^count/)).not.toHaveAttribute('aria-required');
    expect(screen.getByLabelText(/^count/)).toHaveAttribute('inputmode', 'numeric');
    const mode = screen.getByLabelText(/^mode/);
    expect(mode.tagName).toBe('SELECT');
    expect(within(mode).getByRole('option', { name: 'unset' })).toBeInTheDocument();
    expect(within(mode).getByRole('option', { name: 'fast' })).toBeInTheDocument();
    expect(screen.getByLabelText(/^verbose/).tagName).toBe('SELECT');
    expect(screen.getByLabelText(/^tags/).tagName).toBe('TEXTAREA');
    expect(screen.getByText('Page title')).toBeInTheDocument();
  });

  it('keeps the disclosure state across tools for the session', () => {
    const { unmount } = renderTryIt();
    expand();
    unmount();
    renderTryIt({ tool: { name: 'other', annotations: { readOnlyHint: true } } as ToolRow });
    expect(screen.getByRole('button', { name: 'Try it' })).toHaveAttribute('aria-expanded', 'true');
  });

  it('offers only JSON pre-filled with {} when there is no schema', async () => {
    renderTryIt({ schema: undefined });
    expand();
    expect(screen.queryByRole('group', { name: 'Argument editor' })).not.toBeInTheDocument();
    expect(await screen.findByLabelText('JSON value')).toHaveValue('{}');
  });

  it('says a property-less schema takes no arguments and still runs', async () => {
    const fetchMock = stubFetch();
    renderTryIt({ schema: { type: 'object' } });
    expand();
    expect(screen.getByText('This tool takes no arguments')).toBeInTheDocument();
    fireEvent.click(runButton());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(sentBody(fetchMock).arguments).toEqual({});
  });

  it('omits unset optional fields and fills defaults only on request', async () => {
    const fetchMock = stubFetch();
    renderTryIt();
    expand();
    expect(screen.getByLabelText(/^count/)).toHaveAttribute('placeholder', 'Default: 5');
    fireEvent.change(screen.getByLabelText(/^title/), { target: { value: 'Home' } });
    fireEvent.click(runButton());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(sentBody(fetchMock).arguments).toEqual({ title: 'Home' });

    fireEvent.click(screen.getByRole('button', { name: 'Insert defaults' }));
    expect(screen.getByLabelText(/^count/)).toHaveValue('5');
  });
});

describe('ToolTryIt — validation', () => {
  it('shows Required on the empty required field, focuses it, and sends nothing', () => {
    const fetchMock = stubFetch();
    renderTryIt();
    expand();
    fireEvent.click(runButton());
    const title = screen.getByLabelText(/^title/);
    expect(title).toHaveAttribute('aria-invalid', 'true');
    expect(title).toHaveFocus();
    const describedBy = title.getAttribute('aria-describedby') ?? '';
    const error = describedBy
      .split(' ')
      .map((id) => document.getElementById(id))
      .find((el) => el?.textContent === 'Required');
    expect(error).toBeTruthy();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('shows Required after blur before any Run', () => {
    renderTryIt();
    expand();
    expect(screen.queryByText('Required')).not.toBeInTheDocument();
    fireEvent.blur(screen.getByLabelText(/^title/));
    expect(screen.getByText('Required')).toBeInTheDocument();
  });

  it('rejects text in an integer field and a value below minimum', () => {
    const fetchMock = stubFetch();
    renderTryIt();
    expand();
    fireEvent.change(screen.getByLabelText(/^title/), { target: { value: 'x' } });
    fireEvent.change(screen.getByLabelText(/^count/), { target: { value: 'abc' } });
    fireEvent.click(runButton());
    expect(screen.getByText('Must be an integer')).toBeInTheDocument();
    expect(screen.getByLabelText(/^count/)).toHaveFocus();

    fireEvent.change(screen.getByLabelText(/^count/), { target: { value: '0' } });
    expect(screen.getByText('Must be at least 1')).toBeInTheDocument();
    fireEvent.click(runButton());
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('notes unchecked keywords and runs a oneOf or $ref schema from valid JSON', async () => {
    const fetchMock = stubFetch();
    renderTryIt({
      schema: {
        type: 'object',
        properties: {
          target: { oneOf: [{ type: 'string' }, { type: 'number' }] },
          ref: { $ref: '#/$defs/thing' },
        },
      },
    });
    expand();
    expect(
      screen.getByText('Parts of this schema are not checked in the browser. The server validates the call.'),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'JSON' }));
    fireEvent.change(await screen.findByLabelText('JSON value'), {
      target: { value: '{"target": true, "ref": {"a": 1}}' },
    });
    fireEvent.click(runButton());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    expect(sentBody(fetchMock).arguments).toEqual({ target: true, ref: { a: 1 } });
  });

  it('lists JSON-mode errors under the editor and sends nothing', async () => {
    const fetchMock = stubFetch();
    renderTryIt({ schema: undefined });
    expand();
    fireEvent.change(await screen.findByLabelText('JSON value'), { target: { value: '[1]' } });
    fireEvent.click(runButton());
    expect(within(screen.getByRole('list', { name: 'Argument errors' })).getByText('Arguments must be a JSON object')).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('ToolTryIt — Form and JSON toggle', () => {
  it('preserves values and unknown properties, and refuses invalid JSON', async () => {
    renderTryIt();
    expand();
    fireEvent.change(screen.getByLabelText(/^title/), { target: { value: 'Home' } });
    fireEvent.change(screen.getByLabelText(/^count/), { target: { value: '7' } });
    fireEvent.click(screen.getByRole('button', { name: 'JSON' }));
    const editor = await screen.findByLabelText('JSON value');
    expect(JSON.parse((editor as HTMLTextAreaElement).value)).toEqual({ title: 'Home', count: 7 });

    fireEvent.change(editor, { target: { value: '{"title": "Home", "count": 7, "extra": {"k": 1}}' } });
    fireEvent.click(screen.getByRole('button', { name: 'Form' }));
    expect(screen.getByLabelText(/^title/)).toHaveValue('Home');
    expect(screen.getByLabelText(/^count/)).toHaveValue('7');
    expect(screen.getByText('Additional arguments')).toBeInTheDocument();
    expect(screen.getByText(/"k": 1/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'JSON' }));
    const again = await screen.findByLabelText('JSON value');
    expect(JSON.parse((again as HTMLTextAreaElement).value)).toEqual({ title: 'Home', count: 7, extra: { k: 1 } });

    fireEvent.change(again, { target: { value: '{not json' } });
    fireEvent.click(screen.getByRole('button', { name: 'Form' }));
    expect(screen.getByText('The JSON must be a valid object before switching to Form.')).toBeInTheDocument();
    expect(screen.getByLabelText('JSON value')).toBeInTheDocument();
  });
});

describe('ToolTryIt — confirmation', () => {
  const needsConfirm: [string, ToolRow['annotations']][] = [
    ['destructiveHint true', { destructiveHint: true }],
    ['no annotations', undefined],
    ['readOnlyHint false without destructiveHint', { readOnlyHint: false }],
  ];
  it.each(needsConfirm)('confirms for %s; cancelling sends nothing', async (_label, annotations) => {
    const fetchMock = stubFetch();
    renderTryIt({ tool: { name: 'del', annotations } as ToolRow, schema: undefined });
    expand();
    runButton().focus();
    fireEvent.click(runButton());
    const dialog = screen.getByRole('alertdialog');
    expect(within(dialog).getByText('wiki__del')).toBeInTheDocument();
    // useFocusTrap applies initial focus one animation frame later.
    await waitFor(() => expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus());
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(runButton()).toHaveFocus();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('sends after the dialog is confirmed', async () => {
    const fetchMock = stubFetch();
    renderTryIt({ tool: { name: 'del' } as ToolRow, schema: undefined });
    expand();
    fireEvent.click(runButton());
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Run' }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  });

  const noConfirm: [string, ToolRow['annotations']][] = [
    ['readOnlyHint true', { readOnlyHint: true }],
    ['destructiveHint false', { destructiveHint: false }],
  ];
  it.each(noConfirm)('runs without a dialog for %s', async (_label, annotations) => {
    const fetchMock = stubFetch();
    renderTryIt({ tool: { name: 'safe', annotations } as ToolRow, schema: undefined });
    expand();
    fireEvent.click(runButton());
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
  });
});

describe('ToolTryIt — request', () => {
  it('sends exactly name, arguments, and client ui', async () => {
    const fetchMock = stubFetch();
    renderTryIt();
    expand();
    fireEvent.change(screen.getByLabelText(/^title/), { target: { value: 'Home' } });
    fireEvent.keyDown(screen.getByLabelText(/^title/), { key: 'Enter' });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const body = sentBody(fetchMock);
    expect(body).toEqual({ name: 'wiki__get_page', arguments: { title: 'Home' }, client: 'ui' });
    expect(Object.keys(body).sort()).toEqual(['arguments', 'client', 'name']);
  });

  it('refuses an oversized body locally', async () => {
    const fetchMock = stubFetch();
    renderTryIt({ schema: undefined });
    expand();
    fireEvent.change(await screen.findByLabelText('JSON value'), {
      target: { value: JSON.stringify({ blob: 'x'.repeat(2 * 1024 * 1024) }) },
    });
    fireEvent.click(runButton());
    expect(screen.getByText(/larger than the 2 MiB request limit/)).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('ToolTryIt — disabled states', () => {
  it('disables Run with a described reason when the saved allow-list hides the tool', () => {
    renderTryIt({ exposed: false });
    expand();
    const run = runButton();
    expect(run).toBeDisabled();
    const reason = document.getElementById(run.getAttribute('aria-describedby') ?? '');
    expect(reason).toHaveTextContent('Not exposed. This tool is hidden by the saved allow-list for wiki');
  });

  it('shows the draft note without disabling Run', () => {
    renderTryIt({ draftDiffers: true });
    expand();
    expect(
      screen.getByText('Unsaved allow-list changes are not applied. Calls use the saved allow-list.'),
    ).toBeInTheDocument();
    expect(runButton()).toBeEnabled();
  });

  it.each(['block', 'require_approval'])('disables Run for a %s policy verdict', (action) => {
    renderTryIt({ policy: { action, source: 'rule', rule: 'no-wiki', exposed: true } });
    expand();
    const run = runButton();
    expect(run).toBeDisabled();
    const reason = document.getElementById(run.getAttribute('aria-describedby') ?? '');
    expect(reason?.textContent).toMatch(/no-wiki/);
  });

  it('disables Run while the gateway credential is pending', () => {
    useAuthStore.setState({ authRequired: true });
    renderTryIt();
    expand();
    expect(runButton()).toBeDisabled();
    expect(screen.getByText(/gateway credential is verified/)).toBeInTheDocument();
  });
});

async function runWith(status: number, body: unknown) {
  stubFetch(status, body);
  renderTryIt({ schema: undefined });
  expand();
  fireEvent.click(runButton());
  return screen.findByTestId('tool-call-result');
}

function denial(code: string, message = `msg ${code}`) {
  return {
    ...OK,
    outcome: { disposition: 'denied', stage: 'gate', reason: code, completion: 'not_started' },
    result: null,
    error: { code, message },
  };
}

describe('ToolTryIt — results', () => {
  it('renders a policy denial at 200 with its sentence', async () => {
    const result = await runWith(200, denial('policy_blocked'));
    expect(result).toHaveAttribute('data-outcome', 'not_run');
    expect(within(result).getByText('A `policy:` rule blocks this tool.')).toBeInTheDocument();
    expect(within(result).queryByText(/^Completed/)).not.toBeInTheDocument();
  });

  it('renders client_scope with its remedy', async () => {
    const result = await runWith(200, denial('client_scope'));
    expect(within(result).getByText(/Add a `ui` profile under `clients:`/)).toBeInTheDocument();
  });

  it.each([
    ['no_replica', 'No healthy replica is available for this server.'],
    ['cold_start', 'The server failed to start.'],
  ])('renders %s with its sentence', async (code, sentence) => {
    const result = await runWith(200, { ...denial(code), outcome: { disposition: 'failed', stage: 'route', reason: code, completion: 'not_started' } });
    expect(within(result).getByText(sentence)).toBeInTheDocument();
  });

  it('falls back to the message for an unmapped 400 code', async () => {
    const result = await runWith(400, {
      ...denial('invalid_name', 'name must be server__tool'),
      outcome: { disposition: 'rejected', stage: 'request', reason: 'invalid_name', completion: 'not_started' },
    });
    expect(within(result).getByText(/name must be server__tool/)).toBeInTheDocument();
    expect(within(result).getByText(/invalid_name/)).toBeInTheDocument();
  });

  it('renders a tool error with error styling and its content', async () => {
    const result = await runWith(200, {
      ...OK,
      outcome: { disposition: 'tool_error', stage: 'downstream', reason: 'tool_error', completion: 'complete' },
      result: { isError: true, content: [{ type: 'text', text: 'boom' }] },
    });
    expect(result).toHaveAttribute('data-outcome', 'tool_error');
    expect(within(result).getByText(/Tool returned an error/)).toBeInTheDocument();
    expect(within(result).getByText('boom')).toBeInTheDocument();
  });

  it('caps a 200,000 character result until Show all and copies the full text', async () => {
    const big = 'a'.repeat(200_000);
    const result = await runWith(200, { ...OK, result: { content: [{ type: 'text', text: big }] } });
    const bodyEl = within(result).getByTestId('tool-call-result-body');
    expect(bodyEl.textContent!.length).toBeLessThanOrEqual(20_000);
    expect(within(result).getByText(/Showing the first 20,000 of 200,000 characters/)).toBeInTheDocument();

    fireEvent.click(within(result).getByRole('button', { name: /Copy result/ }));
    await waitFor(() => expect(copyTextToClipboard).toHaveBeenCalledWith(big));
    expect(await within(result).findByText('Copied')).toBeInTheDocument();

    fireEvent.click(within(result).getByRole('button', { name: 'Show all' }));
    expect(within(result).getByTestId('tool-call-result-body').textContent!.length).toBe(200_000);
  });

  it('summarizes non-text blocks without creating media or links from tool output', async () => {
    const result = await runWith(200, {
      ...OK,
      result: {
        content: [
          { type: 'image', mimeType: 'image/png', data: 'aGVsbG8gd29ybGQ=' },
          { type: 'text', text: '<a href="https://evil.example">x</a><img src="https://evil.example/i.png">' },
        ],
      },
    });
    expect(result.querySelector('img')).toBeNull();
    expect(result.querySelector('a[href^="https://evil"]')).toBeNull();
    expect(within(result).getByText(/image/)).toBeInTheDocument();
    expect(within(result).getByText(/image\/png/)).toBeInTheDocument();
  });

  it('links to the exact trace when trace_id is present', async () => {
    const result = await runWith(200, { ...OK, trace_id: 'abc123' });
    expect(within(result).getByRole('link', { name: /View trace$/ })).toHaveAttribute('href', '/traces?trace=abc123');
  });

  it('links to traces for the tool without trace_id', async () => {
    const result = await runWith(200, OK);
    expect(within(result).getByRole('link', { name: /View traces for this tool/ })).toHaveAttribute(
      'href',
      '/traces?q=get_page&server=wiki',
    );
  });

  it('announces the outcome through a polite live region', async () => {
    await runWith(200, OK);
    const status = screen.getByRole('status');
    expect(status).toHaveAttribute('aria-live', 'polite');
    expect(status.textContent).toMatch(/^Completed/);
  });
});

describe('ToolTryIt — cancel', () => {
  it('Cancel aborts, shows the cancelled message, and ignores a late response', async () => {
    const pending = pendingFetch();
    renderTryIt({ schema: undefined });
    expand();
    fireEvent.click(runButton());
    await waitFor(() => expect(pending.fetchMock).toHaveBeenCalled());
    expect(screen.getByRole('status')).toHaveTextContent('Running');
    expect(await screen.findByLabelText('JSON value')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(pending.signal().aborted).toBe(true);
    expect(screen.getAllByText(CANCELLED_MESSAGE).length).toBeGreaterThan(0);
    await act(async () => {
      pending.release(respond(200, OK));
    });
    expect(screen.queryByTestId('tool-call-result')).not.toBeInTheDocument();
  });

  it('Escape inside the section aborts without reaching the shell', async () => {
    useUIStore.setState({ sidebarOpen: true });
    const onWindowKey = vi.fn();
    window.addEventListener('keydown', onWindowKey);
    try {
      const pending = pendingFetch();
      renderTryIt({ schema: undefined });
      expand();
      fireEvent.click(runButton());
      await waitFor(() => expect(pending.fetchMock).toHaveBeenCalled());
      fireEvent.keyDown(screen.getByRole('button', { name: 'Cancel' }), { key: 'Escape' });
      expect(pending.signal().aborted).toBe(true);
      expect(screen.getAllByText(CANCELLED_MESSAGE).length).toBeGreaterThan(0);
      expect(onWindowKey).not.toHaveBeenCalled();
      expect(useUIStore.getState().sidebarOpen).toBe(true);
    } finally {
      window.removeEventListener('keydown', onWindowKey);
    }
  });

  it('does not intercept Escape when no call is running', () => {
    const onWindowKey = vi.fn();
    window.addEventListener('keydown', onWindowKey);
    try {
      renderTryIt();
      expand();
      fireEvent.keyDown(runButton(), { key: 'Escape' });
      expect(onWindowKey).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener('keydown', onWindowKey);
    }
  });

  it('marks inputs read-only while running', async () => {
    const pending = pendingFetch();
    renderTryIt();
    expand();
    fireEvent.change(screen.getByLabelText(/^title/), { target: { value: 'Home' } });
    fireEvent.click(runButton());
    await waitFor(() => expect(pending.fetchMock).toHaveBeenCalled());
    expect(screen.getByLabelText(/^title/)).toHaveAttribute('readonly');
    expect(screen.getByLabelText(/^verbose/)).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Run' })).not.toBeInTheDocument();
  });
});

describe('ToolTryIt — in the detail panel', () => {
  function panel(tool: ToolRow) {
    return (
      <ToolDetailPanel
        serverName="wiki"
        tool={tool}
        enabled
        auditMode={false}
        auditState={null}
        onClose={() => {}}
      />
    );
  }

  it('renders Try it above Description', () => {
    render(panel({ ...READ_ONLY, description: 'Reads a page' }));
    const tryIt = screen.getByRole('button', { name: 'Try it' });
    const description = screen.getByText('Description');
    expect(tryIt.compareDocumentPosition(description) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('selecting another tool aborts the call', async () => {
    const pending = pendingFetch();
    const { rerender } = render(panel(READ_ONLY));
    expand();
    fireEvent.click(runButton());
    await waitFor(() => expect(pending.fetchMock).toHaveBeenCalled());
    rerender(panel({ name: 'other', annotations: { readOnlyHint: true } } as ToolRow));
    expect(pending.signal().aborted).toBe(true);
    expect(screen.queryByText(CANCELLED_MESSAGE)).not.toBeInTheDocument();
    expect(runButton()).toBeEnabled();
  });

  it('closing the rail aborts the call', async () => {
    const pending = pendingFetch();
    const { unmount } = render(panel(READ_ONLY));
    expand();
    fireEvent.click(runButton());
    await waitFor(() => expect(pending.fetchMock).toHaveBeenCalled());
    unmount();
    expect(pending.signal().aborted).toBe(true);
  });
});

describe('ToolTryIt — history', () => {
  it('keeps five recent sets per tool, loads one without sending, and stays out of storage', async () => {
    const fetchMock = stubFetch();
    renderTryIt({ schema: undefined });
    expand();
    const editor = await screen.findByLabelText('JSON value');
    expect(screen.getByLabelText('History')).toBeDisabled();
    for (let i = 1; i <= 6; i++) {
      fireEvent.change(editor, { target: { value: JSON.stringify({ secret: `s${i}` }) } });
      fireEvent.click(runButton());
      await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(i));
      await screen.findByTestId('tool-call-result');
    }
    // A repeat collapses into one entry.
    fireEvent.click(runButton());
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(7));

    const history = screen.getByLabelText('History');
    const options = within(history).getAllByRole('option').slice(1);
    expect(options).toHaveLength(5);
    expect(options[0].textContent).toContain('s6');
    expect(options.some((o) => o.textContent?.includes('"s1"'))).toBe(false);

    fireEvent.change(history, { target: { value: '4' } });
    expect(JSON.parse((screen.getByLabelText('JSON value') as HTMLTextAreaElement).value)).toEqual({ secret: 's2' });
    expect(fetchMock).toHaveBeenCalledTimes(7);

    expect(useToolCallHistoryStore.getState().list('wiki__other')).toHaveLength(0);
    const stored = [
      ...Object.keys(localStorage).map((k) => localStorage.getItem(k)),
      ...Object.keys(sessionStorage).map((k) => sessionStorage.getItem(k)),
    ].join('');
    expect(stored).not.toMatch(/s[1-6]/);
    expect(window.location.href).not.toMatch(/secret/);

    act(() => useAuthStore.setState({ generation: Symbol() }));
    expect(screen.getByLabelText('History')).toBeDisabled();
  });
});
