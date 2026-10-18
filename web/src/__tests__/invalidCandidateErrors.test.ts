import { describe, it, expect, vi, afterEach } from 'vitest';
import { HTTPError, triggerReload } from '../lib/api';

describe('invalid_candidate errors', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  function mockResponse(status: number, body: unknown) {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(Response.json(body, { status })),
    );
  }

  it('attaches errors from an invalid_candidate envelope', async () => {
    mockResponse(400, {
      code: 'invalid_candidate',
      message: 'candidate configuration could not be resolved or validated; active configuration is unchanged',
      errors: ["groups[ops].tools[0]: tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called", 12],
    });

    const err = await triggerReload().catch((e) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).code).toBe('invalid_candidate');
    expect((err as HTTPError).errors).toEqual([
      "groups[ops].tools[0]: tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called",
    ]);
  });

  it('leaves errors unset when the envelope omits them', async () => {
    mockResponse(400, {
      code: 'invalid_candidate',
      message: 'candidate configuration could not be resolved or validated; active configuration is unchanged',
    });

    const err = await triggerReload().catch((e) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).errors).toBeUndefined();
  });
});
