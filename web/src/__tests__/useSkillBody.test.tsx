import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useSkillBody } from '../hooks/useSkillBody';
import { fetchRegistrySkill } from '../lib/api';
import type { AgentSkill } from '../types';

vi.mock('../lib/api', () => ({
  fetchRegistrySkill: vi.fn(),
}));

const skill = (body: string): AgentSkill => ({
  name: 'incident-triage',
  description: '',
  state: 'active',
  body,
  fileCount: 0,
});

function deferred<T>() {
  let resolve: (value: T) => void = () => {};
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

describe('useSkillBody', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill('# Fetched'));
  });

  it('fetches the body when there is no seed', async () => {
    const { result } = renderHook(() => useSkillBody('incident-triage', true));
    expect(result.current.loading).toBe(true);
    expect(result.current.body).toBeNull();

    await waitFor(() => expect(result.current.body).toBe('# Fetched'));
    expect(result.current.loading).toBe(false);
    expect(fetchRegistrySkill).toHaveBeenCalledTimes(1);
  });

  it('adopts a seed without fetching', () => {
    const { result } = renderHook(() => useSkillBody('incident-triage', true, '# Seeded'));
    expect(result.current).toEqual({ body: '# Seeded', loading: false, error: null });
    expect(fetchRegistrySkill).not.toHaveBeenCalled();
  });

  // '' is a real answer ("no instructions"), not a missing value, so it must
  // not trigger a fetch that would replace it.
  it('treats an empty-string seed as hydrated', () => {
    const { result } = renderHook(() => useSkillBody('incident-triage', true, ''));
    expect(result.current).toEqual({ body: '', loading: false, error: null });
    expect(fetchRegistrySkill).not.toHaveBeenCalled();
  });

  it('does nothing while disabled', () => {
    const { result } = renderHook(() => useSkillBody('incident-triage', false));
    expect(result.current).toEqual({ body: null, loading: false, error: null });
    expect(fetchRegistrySkill).not.toHaveBeenCalled();
  });

  it('does nothing without a skill name', () => {
    renderHook(() => useSkillBody(null, true));
    expect(fetchRegistrySkill).not.toHaveBeenCalled();
  });

  // A failed load must stay null rather than collapsing to '': callers use
  // "body is known" to decide whether writing it back is safe.
  it('reports an error without inventing an empty body', async () => {
    vi.mocked(fetchRegistrySkill).mockRejectedValue(new Error('registry offline'));
    const { result } = renderHook(() => useSkillBody('incident-triage', true));

    await waitFor(() => expect(result.current.error).toBe('registry offline'));
    expect(result.current.body).toBeNull();
    expect(result.current.loading).toBe(false);
  });

  it('refetches and re-reports loading when the skill changes', async () => {
    const { result, rerender } = renderHook(({ name }) => useSkillBody(name, true), {
      initialProps: { name: 'incident-triage' },
    });
    await waitFor(() => expect(result.current.body).toBe('# Fetched'));

    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill('# Second'));
    rerender({ name: 'other-skill' });
    // The previous skill's body is not shown against the new selection.
    expect(result.current.body).toBeNull();
    expect(result.current.loading).toBe(true);

    await waitFor(() => expect(result.current.body).toBe('# Second'));
    expect(fetchRegistrySkill).toHaveBeenLastCalledWith('other-skill');
  });

  // The editor's shape on close and reopen: the closed editor drops the name
  // and passes a '' seed, then reopens on the same skill.
  it('drops the previous body and reports loading when re-enabled', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill('# First'));
    const { result, rerender } = renderHook(
      ({ name, on }: { name: string | null; on: boolean }) =>
        useSkillBody(name, on, on ? undefined : ''),
      { initialProps: { name: 'incident-triage' as string | null, on: true } },
    );
    await waitFor(() => expect(result.current.body).toBe('# First'));

    rerender({ name: null, on: false });
    const second = deferred<AgentSkill>();
    vi.mocked(fetchRegistrySkill).mockReturnValue(second.promise);
    rerender({ name: 'incident-triage', on: true });
    expect(result.current).toEqual({ body: null, loading: true, error: null });

    second.resolve(skill('# Second'));
    await waitFor(() => expect(result.current.body).toBe('# Second'));
    expect(result.current.loading).toBe(false);
  });

  // The sidebar row's shape: the name stays put and only `enabled` toggles.
  it('refetches and reports loading when collapsed and re-expanded', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill('# First'));
    const { result, rerender } = renderHook(({ on }) => useSkillBody('incident-triage', on), {
      initialProps: { on: true },
    });
    await waitFor(() => expect(result.current.body).toBe('# First'));

    rerender({ on: false });
    const second = deferred<AgentSkill>();
    vi.mocked(fetchRegistrySkill).mockReturnValue(second.promise);
    rerender({ on: true });
    expect(fetchRegistrySkill).toHaveBeenCalledTimes(2);
    expect(result.current).toEqual({ body: null, loading: true, error: null });

    second.resolve(skill('# Second'));
    await waitFor(() => expect(result.current.body).toBe('# Second'));
  });

  it('refetches on a revision bump while the previous body stays visible', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill('# First'));
    const { result, rerender } = renderHook(
      ({ revision }) => useSkillBody('incident-triage', true, undefined, revision),
      { initialProps: { revision: 0 } },
    );
    await waitFor(() => expect(result.current.body).toBe('# First'));

    const second = deferred<AgentSkill>();
    vi.mocked(fetchRegistrySkill).mockReturnValue(second.promise);
    rerender({ revision: 1 });
    expect(fetchRegistrySkill).toHaveBeenCalledTimes(2);
    expect(result.current).toEqual({ body: '# First', loading: false, error: null });

    second.resolve(skill('# Second'));
    await waitFor(() => expect(result.current.body).toBe('# Second'));
    expect(result.current.loading).toBe(false);
  });

  it('replaces the visible body with the error when a revision refetch fails', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill('# First'));
    const { result, rerender } = renderHook(
      ({ revision }) => useSkillBody('incident-triage', true, undefined, revision),
      { initialProps: { revision: 0 } },
    );
    await waitFor(() => expect(result.current.body).toBe('# First'));

    vi.mocked(fetchRegistrySkill).mockRejectedValue(new Error('registry offline'));
    rerender({ revision: 1 });
    await waitFor(() => expect(result.current.error).toBe('registry offline'));
    expect(result.current.body).toBeNull();
    expect(result.current.loading).toBe(false);
  });

  it('never stores a response superseded by a revision bump', async () => {
    const first = deferred<AgentSkill>();
    const second = deferred<AgentSkill>();
    vi.mocked(fetchRegistrySkill)
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise);
    const { result, rerender } = renderHook(
      ({ revision }) => useSkillBody('incident-triage', true, undefined, revision),
      { initialProps: { revision: 0 } },
    );

    rerender({ revision: 1 });
    second.resolve(skill('# Second'));
    await waitFor(() => expect(result.current.body).toBe('# Second'));

    first.resolve(skill('# First'));
    await act(async () => {
      await first.promise;
    });
    expect(result.current.body).toBe('# Second');
  });

  it('reports a body of "" when the skill genuinely has none', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill(''));
    const { result } = renderHook(() => useSkillBody('incident-triage', true));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.body).toBe('');
    expect(result.current.error).toBeNull();
  });
});
