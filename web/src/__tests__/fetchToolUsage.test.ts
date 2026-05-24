import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchToolUsage } from '../lib/api';
import type { ToolUsageResponse } from '../types';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('fetchToolUsage', () => {
  it('GETs /api/tools/usage and returns the parsed response', async () => {
    const payload: ToolUsageResponse = {
      observedSince: '2026-05-20T10:00:00Z',
      servers: {
        github: { create_issue: { calls: 2, lastCalledAt: '2026-05-24T09:00:00Z' } },
      },
    };
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => payload,
    });
    vi.stubGlobal('fetch', fetchMock);

    const result = await fetchToolUsage();

    expect(result).toEqual(payload);
    expect(fetchMock).toHaveBeenCalledWith('/api/tools/usage', expect.anything());
  });

  it('throws on a non-ok response so callers can surface the failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 503,
        statusText: 'Service Unavailable',
        json: async () => ({}),
      }),
    );

    await expect(fetchToolUsage()).rejects.toThrow(/503/);
  });
});
