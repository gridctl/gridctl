import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchPythonPackageVersions, HTTPError, resolvePythonSource } from '../lib/api';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('Python source API', () => {
  it('preserves package resolution errors for inline validation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(
      JSON.stringify({ error: 'No PyPI project named missing-package.' }),
      { status: 422, headers: { 'Content-Type': 'application/json' } },
    )));

    await expect(fetchPythonPackageVersions('missing-package')).rejects.toEqual(
      expect.objectContaining<Partial<HTTPError>>({
        status: 422,
        message: 'No PyPI project named missing-package.',
      }),
    );
  });

  it('removes form-only serverType from preview requests', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      declaredIdentity: { type: 'pypi' },
      resolvedIdentity: { type: 'pypi' },
      buildInputDigest: 'abc',
      imageTag: 'gridctl-preview-fetch:0.6.0-abc',
      cached: false,
      mutableRef: false,
    }), { status: 200, headers: { 'Content-Type': 'application/json' } }));
    vi.stubGlobal('fetch', fetchMock);

    await resolvePythonSource({
      name: 'fetch',
      serverType: 'source',
      source: { type: 'pypi', package: 'mcp-server-fetch', ref: '0.6.0' },
    });

    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(request.body as string)).toEqual({
      stackName: 'preview',
      server: {
        name: 'fetch',
        source: { type: 'pypi', package: 'mcp-server-fetch', ref: '0.6.0' },
      },
    });
  });
});
