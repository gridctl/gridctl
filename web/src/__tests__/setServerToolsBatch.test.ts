import { afterEach, describe, expect, it, vi } from 'vitest';
import { setServerToolsBatch, SetServerToolsError } from '../lib/api';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('setServerToolsBatch', () => {
  it('PUTs the servers payload to /api/mcp-servers/tools and returns the result', async () => {
    const payload = {
      servers: [{ server: 'github', tools: ['a'] }],
      reloaded: true,
      reloadedAt: '2026-05-24T10:00:00Z',
    };
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => payload });
    vi.stubGlobal('fetch', fetchMock);

    const result = await setServerToolsBatch([{ name: 'github', tools: ['a'] }]);

    expect(result).toEqual(payload);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/mcp-servers/tools');
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body)).toEqual({ servers: [{ name: 'github', tools: ['a'] }] });
  });

  it('throws a SetServerToolsError on a structured envelope', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: false,
        status: 409,
        statusText: 'Conflict',
        json: async () => ({ error: { code: 'stack_modified', message: 'changed', hint: 'reload' } }),
      }),
    );

    await expect(setServerToolsBatch([{ name: 'github', tools: [] }])).rejects.toMatchObject({
      name: 'SetServerToolsError',
      code: 'stack_modified',
      httpStatus: 409,
    });
    // Constructor sanity: the class is the one we import.
    expect(new SetServerToolsError('x', 'y', undefined, 400)).toBeInstanceOf(Error);
  });
});
