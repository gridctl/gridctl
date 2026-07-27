import { describe, it, expect, beforeEach, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
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

  it('reports a body of "" when the skill genuinely has none', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue(skill(''));
    const { result } = renderHook(() => useSkillBody('incident-triage', true));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.body).toBe('');
    expect(result.current.error).toBeNull();
  });
});
