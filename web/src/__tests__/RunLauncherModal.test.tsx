import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react';
import '@testing-library/jest-dom';

vi.mock('../lib/agent-runs', async () => {
  const actual = await vi.importActual<typeof import('../lib/agent-runs')>(
    '../lib/agent-runs',
  );
  return {
    ...actual,
    fetchAgentRuns: vi.fn().mockResolvedValue({ runs: [] }),
    fetchAgentRun: vi.fn().mockResolvedValue(null),
    launchRun: vi.fn(),
  };
});

import { RunLauncherModal } from '../components/agent/ide/RunLauncherModal';
import {
  fetchAgentRuns,
  fetchAgentRun,
  launchRun,
  LaunchRunError,
} from '../lib/agent-runs';

const mockedLaunchRun = vi.mocked(launchRun);
const mockedFetchAgentRuns = vi.mocked(fetchAgentRuns);
const mockedFetchAgentRun = vi.mocked(fetchAgentRun);

beforeEach(() => {
  vi.clearAllMocks();
  mockedFetchAgentRuns.mockResolvedValue({ runs: [] });
  mockedFetchAgentRun.mockResolvedValue(null);
  window.localStorage.clear();
});

afterEach(() => {
  cleanup();
});

function renderModal(overrides: {
  skill?: { name: string; description?: string; inputSchema?: Record<string, unknown> };
  onClose?: () => void;
  onLaunched?: (r: { run_id: string; started_at: string }) => void;
} = {}) {
  const onClose = overrides.onClose ?? vi.fn();
  const onLaunched = overrides.onLaunched ?? vi.fn();
  const skill = overrides.skill ?? { name: 'repo-audit' };
  render(
    <RunLauncherModal skill={skill} onClose={onClose} onLaunched={onLaunched} />,
  );
  return { onClose, onLaunched };
}

describe('RunLauncherModal', () => {
  it('renders the skill name in the header', () => {
    renderModal({ skill: { name: 'repo-audit' } });
    expect(screen.getByRole('heading', { name: 'repo-audit' })).toBeInTheDocument();
  });

  it('renders the description when provided', () => {
    renderModal({ skill: { name: 'repo-audit', description: 'audits repos for risk' } });
    expect(screen.getByText('audits repos for risk')).toBeInTheDocument();
  });

  it('pre-fills the JSON editor with {} on first open', () => {
    renderModal();
    const editor = screen.getByLabelText(/input json/i) as HTMLTextAreaElement;
    expect(editor.value).toBe('{}');
  });

  it('restores last input from localStorage', () => {
    window.localStorage.setItem(
      'gridctl.agent.lastInput.repo-audit',
      '{\n  "url": "https://example.com"\n}',
    );
    renderModal({ skill: { name: 'repo-audit' } });
    const editor = screen.getByLabelText(/input json/i) as HTMLTextAreaElement;
    expect(editor.value).toContain('"url": "https://example.com"');
  });

  it('shows an inline parse error and disables Run for invalid JSON', async () => {
    renderModal();
    const editor = screen.getByLabelText(/input json/i) as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: '{ "bad": ' } });
    await waitFor(() => {
      expect(document.getElementById('run-launcher-json-error')).not.toBeNull();
    });
    const run = screen.getByRole('button', { name: /^run$/i });
    expect(run).toBeDisabled();
  });

  it('rejects non-object JSON (arrays, scalars)', async () => {
    renderModal();
    const editor = screen.getByLabelText(/input json/i) as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: '[1, 2, 3]' } });
    await waitFor(() => {
      expect(screen.getByText(/must be a JSON object/i)).toBeInTheDocument();
    });
  });

  it('POSTs the parsed input and calls onLaunched on success', async () => {
    mockedLaunchRun.mockResolvedValueOnce({
      run_id: 'run-abc-123',
      started_at: '2026-05-13T17:30:00Z',
    });
    const { onLaunched } = renderModal();
    const editor = screen.getByLabelText(/input json/i) as HTMLTextAreaElement;
    fireEvent.change(editor, { target: { value: '{"url":"https://example.com"}' } });
    fireEvent.click(screen.getByRole('button', { name: /^run$/i }));
    await waitFor(() => {
      expect(mockedLaunchRun).toHaveBeenCalledWith({
        skill_name: 'repo-audit',
        input: { url: 'https://example.com' },
      });
    });
    await waitFor(() => {
      expect(onLaunched).toHaveBeenCalledWith({
        run_id: 'run-abc-123',
        started_at: '2026-05-13T17:30:00Z',
      });
    });
    expect(window.localStorage.getItem('gridctl.agent.lastInput.repo-audit')).toContain(
      'example.com',
    );
  });

  it('renders the server error in an alert banner on 4xx', async () => {
    mockedLaunchRun.mockRejectedValueOnce(
      new LaunchRunError(422, 'skill "x" has a Go handler; the launcher does not yet support Go plugins'),
    );
    renderModal({ skill: { name: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: /^run$/i }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Go handler');
  });

  it('closes when the user presses ESC', () => {
    const { onClose } = renderModal();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });

  it('closes when Cancel is clicked', () => {
    const { onClose } = renderModal();
    fireEvent.click(screen.getByRole('button', { name: /cancel/i }));
    expect(onClose).toHaveBeenCalled();
  });

  it('shows the Form tab when the schema has properties', () => {
    renderModal({
      skill: {
        name: 'x',
        inputSchema: {
          type: 'object',
          properties: { url: { type: 'string' } },
        },
      },
    });
    expect(screen.getByRole('tab', { name: /form/i })).toBeInTheDocument();
  });

  it('hides the Form tab when the schema is the permissive default', () => {
    renderModal({
      skill: { name: 'x', inputSchema: { type: 'object' } },
    });
    expect(screen.queryByRole('tab', { name: /form/i })).not.toBeInTheDocument();
  });

  it('populates the Run-like picker with prior runs of this skill', async () => {
    mockedFetchAgentRuns.mockResolvedValueOnce({
      runs: [
        {
          run_id: 'a-prev-1',
          skill: 'repo-audit',
          status: 'completed',
          started_at: '2026-05-13T16:00:00Z',
          event_count: 4,
        },
        {
          run_id: 'b-other',
          skill: 'something-else',
          status: 'completed',
          started_at: '2026-05-13T16:30:00Z',
          event_count: 2,
        },
      ],
    });
    renderModal();
    await waitFor(() => {
      // The picker option label includes the short run id "a-prev-1"
      // (first 8 chars). The other-skill run is filtered out.
      const options = screen.getAllByRole('option');
      const found = options.some((o) => /a-prev-1/.test(o.textContent ?? ''));
      expect(found).toBe(true);
      const cross = options.some((o) => /b-other/.test(o.textContent ?? ''));
      expect(cross).toBe(false);
    });
  });
});
