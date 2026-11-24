import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { AgentEditor } from '../components/registry/agents/AgentEditor';
import { bodyFromRaw } from '../components/registry/agents/agentModel';
import { fetchRegistryAgent, updateRegistryAgent } from '../lib/api';
import { useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import type { RegistryAgent } from '../types';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchRegistryAgent: vi.fn(),
    updateRegistryAgent: vi.fn(),
  };
});

vi.mock('../components/registry/agents/agentModel', async () => {
  const actual = await vi.importActual<typeof import('../components/registry/agents/agentModel')>(
    '../components/registry/agents/agentModel',
  );
  return {
    ...actual,
    bodyFromRaw: vi.fn((raw: string) => actual.bodyFromRaw(raw)),
  };
});

const agent: RegistryAgent = {
  name: 'code-reviewer',
  description: 'Reviews code',
};

const raw = '---\nname: code-reviewer\ndescription: d\n---\nBody.';

describe('AgentEditor preview body', () => {
  beforeEach(() => {
    vi.mocked(bodyFromRaw).mockClear();
    vi.mocked(fetchRegistryAgent).mockResolvedValue({ ...agent, raw });
  });

  it('computes bodyFromRaw once per raw value', async () => {
    const { rerender } = render(
      <AgentEditor isOpen agent={agent} onClose={() => {}} onSaved={() => {}} />,
    );
    const textarea = await screen.findByRole('textbox', { name: /AGENT.md content/ });
    await waitFor(() => {
      expect((textarea as HTMLTextAreaElement).value).toContain('code-reviewer');
    });

    const afterLoad = vi.mocked(bodyFromRaw).mock.calls.length;
    expect(afterLoad).toBeGreaterThan(0);

    rerender(<AgentEditor isOpen agent={agent} onClose={() => {}} onSaved={() => {}} />);
    expect(vi.mocked(bodyFromRaw).mock.calls.length).toBe(afterLoad);

    fireEvent.change(textarea, {
      target: { value: '---\nname: code-reviewer\ndescription: d\n---\nEdited body.' },
    });
    expect(vi.mocked(bodyFromRaw).mock.calls.length).toBe(afterLoad + 1);

    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(screen.getByText(/Edited body/)).toBeInTheDocument();
    expect(vi.mocked(bodyFromRaw).mock.calls.length).toBe(afterLoad + 1);
  });
});

describe('AgentEditor unsaved guard', () => {
  beforeEach(() => {
    vi.mocked(fetchRegistryAgent).mockResolvedValue({ ...agent, raw });
    vi.mocked(updateRegistryAgent).mockReset();
    vi.mocked(updateRegistryAgent).mockResolvedValue(undefined as never);
    useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
  });

  afterEach(() => {
    useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
  });

  async function openLoaded(onClose = vi.fn(), onSaved = vi.fn()) {
    render(<AgentEditor isOpen agent={agent} onClose={onClose} onSaved={onSaved} />);
    const textarea = await screen.findByRole('textbox', { name: /AGENT.md content/ });
    await waitFor(() => expect((textarea as HTMLTextAreaElement).value).toContain('code-reviewer'));
    return textarea;
  }

  it('registers a guard and a beforeunload warning once edited', async () => {
    const textarea = await openLoaded();
    expect(useNavigationGuardStore.getState().guards).toHaveLength(0);
    const clean = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);

    fireEvent.change(textarea, { target: { value: raw + '\nMore.' } });
    expect(useNavigationGuardStore.getState().guards).toHaveLength(1);
    expect(useNavigationGuardStore.getState().guards[0].label).toBe('agent code-reviewer');
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);
  });

  it('saves on Ctrl+S only when dirty', async () => {
    const onClose = vi.fn();
    const textarea = await openLoaded(onClose);
    const clean = fireEvent.keyDown(document, { key: 's', ctrlKey: true });
    expect(clean).toBe(false);
    expect(updateRegistryAgent).not.toHaveBeenCalled();

    const edited = raw + '\nMore.';
    fireEvent.change(textarea, { target: { value: edited } });
    fireEvent.keyDown(document, { key: 's', ctrlKey: true });
    await waitFor(() => expect(updateRegistryAgent).toHaveBeenCalledWith('code-reviewer', edited));
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(useNavigationGuardStore.getState().guards).toHaveLength(0));
  });

  it('closes the editor when a guarded navigation is confirmed', async () => {
    const onClose = vi.fn();
    render(<NavigationGuardDialog />);
    const textarea = await openLoaded(onClose);
    fireEvent.change(textarea, { target: { value: raw + '\nMore.' } });
    const proceed = vi.fn();
    act(() => useNavigationGuardStore.getState().guard('/logs', proceed));
    expect(screen.getByText(/unsaved changes in agent code-reviewer/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(proceed).toHaveBeenCalledTimes(1);
  });
});
