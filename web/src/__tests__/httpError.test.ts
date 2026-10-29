import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchStatus, HTTPError, isFeatureUnavailable } from '../lib/api';
import { GatewayRequestError } from '../lib/gatewayRequest';
import { useAuthStore } from '../stores/useAuthStore';

describe('fetchJSON HTTPError', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
  });

  it('rejects a 503 with HTTPError and classifies only that status as unavailable', async () => {
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(Response.json({ error: 'Registry not available' }, { status: 503, statusText: 'Service Unavailable' })),
    );

    const err = await fetchStatus().catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).status).toBe(503);
    expect((err as HTTPError).message.startsWith('API error: 503')).toBe(true);
    expect(isFeatureUnavailable(err)).toBe(true);
    expect(isFeatureUnavailable(new HTTPError(500, 'API error: 500 Internal Server Error'))).toBe(false);
    expect(isFeatureUnavailable(new GatewayRequestError('connection', 'down'))).toBe(false);
    expect(isFeatureUnavailable(new Error('store down'))).toBe(false);
  });
});
