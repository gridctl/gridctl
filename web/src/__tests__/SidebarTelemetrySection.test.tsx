import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, fireEvent, render, screen, within } from '@testing-library/react';

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    updateServerTelemetry: vi.fn(),
    fetchStackSpec: vi.fn(),
    wipeTelemetry: vi.fn(),
  };
});

vi.mock('../components/ui/Toast', async (importActual) => {
  const actual = await importActual<typeof import('../components/ui/Toast')>();
  return { ...actual, showToast: vi.fn() };
});

import { SidebarTelemetrySection } from '../components/telemetry/SidebarTelemetrySection';
import { fetchStackSpec, updateServerTelemetry } from '../lib/api';
import { showToast } from '../components/ui/Toast';
import { useTelemetryStore } from '../stores/useTelemetryStore';
import type { ResolvedTelemetry } from '../types';

const updateMock = vi.mocked(updateServerTelemetry);
const fetchSpecMock = vi.mocked(fetchStackSpec);
const toastMock = vi.mocked(showToast);

const OK = { success: true, inventory: [] };

function seed(config: ResolvedTelemetry) {
  useTelemetryStore.setState({ config, inventory: [], rawSpec: null });
}

function group(signal: 'logs' | 'metrics' | 'traces') {
  return within(screen.getByRole('group', { name: new RegExp(`${signal} persistence`, 'i') }));
}

function segment(signal: 'logs' | 'metrics' | 'traces', label: 'Inherit' | 'On' | 'Off') {
  return group(signal).getByRole('button', { name: label });
}

function allSegments() {
  return (['logs', 'metrics', 'traces'] as const).flatMap((sig) =>
    (['Inherit', 'On', 'Off'] as const).map((label) => segment(sig, label)),
  );
}

async function click(el: HTMLElement) {
  await act(async () => {
    fireEvent.click(el);
  });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  updateMock.mockReset();
  fetchSpecMock.mockReset();
  toastMock.mockReset();
  updateMock.mockResolvedValue(OK);
  fetchSpecMock.mockRejectedValue(new Error('spec unavailable'));
  seed({ global: {}, servers: {} });
});

describe('SidebarTelemetrySection direct selection', () => {
  it('sends one request with false when choosing Off from Inherit', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    expect(updateMock).toHaveBeenCalledTimes(1);
    expect(updateMock).toHaveBeenCalledWith('vault', { persist: { logs: false } });
    expect(updateMock).not.toHaveBeenCalledWith('vault', { persist: { logs: true } });
  });

  it('sends one request with null when choosing Inherit from On', async () => {
    seed({ global: {}, servers: { vault: { logs: true } } });
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Inherit'));
    expect(updateMock).toHaveBeenCalledTimes(1);
    expect(updateMock).toHaveBeenCalledWith('vault', { persist: { logs: null } });
    expect(updateMock).not.toHaveBeenCalledWith('vault', { persist: { logs: false } });
  });

  it('sends nothing when the selected state is chosen again', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Inherit'));
    expect(updateMock).not.toHaveBeenCalled();
  });

  it.each([
    ['inherit', undefined, 'Inherit'],
    ['on', true, 'On'],
    ['off', false, 'Off'],
  ] as const)('presses only the %s segment initially', (_name, value, label) => {
    seed({ global: {}, servers: value === undefined ? {} : { vault: { logs: value } } });
    render(<SidebarTelemetrySection serverName="vault" />);
    const buttons = group('logs').getAllByRole('button');
    expect(buttons).toHaveLength(3);
    const pressed = buttons.filter((b) => b.getAttribute('aria-pressed') === 'true');
    expect(pressed).toHaveLength(1);
    expect(pressed[0]).toHaveAccessibleName(label);
  });

  it('uses buttons in a named group with no radio semantics', () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    expect(screen.queryByRole('radiogroup')).not.toBeInTheDocument();
    expect(screen.queryByRole('radio')).not.toBeInTheDocument();
    for (const button of allSegments()) {
      expect(button.tagName).toBe('BUTTON');
      expect(button).toHaveAttribute('aria-pressed');
      expect(button).not.toHaveAttribute('tabindex');
    }
  });

  it('does not select on arrow keys', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    const inherit = segment('logs', 'Inherit');
    inherit.focus();
    fireEvent.keyDown(inherit, { key: 'ArrowRight' });
    fireEvent.keyDown(inherit, { key: 'ArrowDown' });
    expect(updateMock).not.toHaveBeenCalled();
    expect(inherit).toHaveAttribute('aria-pressed', 'true');
  });

  it('states the global default for an inheriting signal', () => {
    seed({ global: { logs: true }, servers: {} });
    render(<SidebarTelemetrySection serverName="vault" />);
    expect(group('logs').getByText(/global: on/i)).toBeInTheDocument();
    expect(group('metrics').getByText(/global: off/i)).toBeInTheDocument();
  });
});

describe('SidebarTelemetrySection reflection', () => {
  it('shows the chosen segment without a refresh and offers Reset', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    expect(screen.queryByRole('button', { name: /reset to global/i })).not.toBeInTheDocument();
    await click(segment('logs', 'Off'));
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'true');
    expect(segment('logs', 'Inherit')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByRole('button', { name: /reset to global/i })).toBeInTheDocument();
    expect(toastMock).toHaveBeenCalledWith('success', 'Logs persistence disabled for vault');
  });

  it('refreshes the spec after a successful change', async () => {
    fetchSpecMock.mockResolvedValue({
      path: '/tmp/stack.yaml',
      content: [
        'name: example',
        'mcp-servers:',
        '  - name: vault',
        '    telemetry:',
        '      persist:',
        '        logs: false',
        '',
      ].join('\n'),
    });
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    await vi.waitFor(() => {
      expect(useTelemetryStore.getState().config.servers.vault).toEqual({ logs: false });
    });
    expect(fetchSpecMock).toHaveBeenCalledTimes(1);
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'true');
  });

  it('reverts to the previous segment and reports a failed change', async () => {
    updateMock.mockRejectedValue(new Error('reload failed'));
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    expect(segment('logs', 'Inherit')).toHaveAttribute('aria-pressed', 'true');
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'false');
    expect(fetchSpecMock).not.toHaveBeenCalled();
    expect(toastMock).toHaveBeenCalledWith('error', expect.any(String), expect.anything());
  });

  it('keeps the first signal when a second signal changes', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    await click(segment('metrics', 'On'));
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'true');
    expect(segment('logs', 'Inherit')).toHaveAttribute('aria-pressed', 'false');
    expect(segment('metrics', 'On')).toHaveAttribute('aria-pressed', 'true');
    await click(segment('logs', 'Inherit'));
    expect(updateMock).toHaveBeenCalledTimes(3);
    expect(updateMock).toHaveBeenLastCalledWith('vault', { persist: { logs: null } });
  });

  it('returns every signal to Inherit after Reset to global', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    await click(screen.getByRole('button', { name: /reset to global/i }));
    expect(updateMock).toHaveBeenLastCalledWith('vault', { persist: null });
    expect(segment('logs', 'Inherit')).toHaveAttribute('aria-pressed', 'true');
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.queryByRole('button', { name: /reset to global/i })).not.toBeInTheDocument();
  });

  it('keeps overrides when Reset to global fails', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    updateMock.mockRejectedValue(new Error('reload failed'));
    await click(screen.getByRole('button', { name: /reset to global/i }));
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: /reset to global/i })).toBeInTheDocument();
  });

  it('keeps earlier selections when a later change fails', async () => {
    render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    updateMock.mockRejectedValue(new Error('reload failed'));
    await click(segment('metrics', 'On'));
    expect(segment('metrics', 'Inherit')).toHaveAttribute('aria-pressed', 'true');
    expect(segment('metrics', 'On')).toHaveAttribute('aria-pressed', 'false');
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'true');
  });
});

describe('SidebarTelemetrySection busy state', () => {
  it('disables every control until the request and the spec refresh settle', async () => {
    const update = deferred<typeof OK>();
    const spec = deferred<{ path: string; content: string }>();
    updateMock.mockReturnValue(update.promise);
    fetchSpecMock.mockReturnValue(spec.promise);
    render(<SidebarTelemetrySection serverName="vault" />);

    await click(segment('logs', 'Off'));
    for (const button of allSegments()) expect(button).toBeDisabled();
    expect(screen.getByRole('group', { name: /logs persistence/i })).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByRole('group', { name: /metrics persistence/i })).not.toHaveAttribute('aria-busy');
    expect(screen.getByRole('button', { name: /reset to global/i })).toBeDisabled();
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'true');

    await click(segment('metrics', 'On'));
    expect(updateMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      update.resolve(OK);
    });
    expect(fetchSpecMock).toHaveBeenCalledTimes(1);
    for (const button of allSegments()) expect(button).toBeDisabled();
    expect(screen.getByRole('button', { name: /reset to global/i })).toBeDisabled();

    await act(async () => {
      spec.reject(new Error('spec unavailable'));
    });
    for (const button of allSegments()) expect(button).toBeEnabled();
    expect(screen.getByRole('button', { name: /reset to global/i })).toBeEnabled();
    expect(screen.getByRole('group', { name: /logs persistence/i })).not.toHaveAttribute('aria-busy');
  });
});

describe('SidebarTelemetrySection server switch', () => {
  it('does not carry optimistic or busy state to another server', async () => {
    const { rerender } = render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'true');
    rerender(<SidebarTelemetrySection serverName="github" />);
    expect(segment('logs', 'Inherit')).toHaveAttribute('aria-pressed', 'true');
    expect(segment('logs', 'Off')).toHaveAttribute('aria-pressed', 'false');
    expect(screen.queryByRole('button', { name: /reset to global/i })).not.toBeInTheDocument();
  });

  it('drops a busy state when the server changes mid-request', async () => {
    updateMock.mockReturnValue(deferred<typeof OK>().promise);
    const { rerender } = render(<SidebarTelemetrySection serverName="vault" />);
    await click(segment('logs', 'Off'));
    expect(segment('logs', 'Off')).toBeDisabled();
    rerender(<SidebarTelemetrySection serverName="github" />);
    for (const button of allSegments()) expect(button).toBeEnabled();
    expect(segment('logs', 'Inherit')).toHaveAttribute('aria-pressed', 'true');
  });
});
