import { describe, it, expect, vi, afterEach } from 'vitest';
import { HTTPError, fetchStackPlan, triggerReload } from '../lib/api';

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

describe('reload and plan error messages', () => {
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

  it('uses the server message for an uncoded reload failure', async () => {
    mockResponse(400, { success: false, message: 'network configuration changed - full restart required' });

    const err = await triggerReload().catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect((err as Error).message).toBe('network configuration changed - full restart required');
  });

  it('falls back to the status when error is an object and message is absent', async () => {
    mockResponse(400, { success: false, error: { detail: 'nested' } });

    const err = await triggerReload().catch((e) => e);
    expect((err as Error).message).toBe('Reload failed: 400');
    expect((err as Error).message).not.toContain('[object Object]');
  });

  it('surfaces the server error from a failed plan', async () => {
    mockResponse(500, { error: 'Failed to load stack file: yaml: line 3: mapping values are not allowed' });

    const err = await fetchStackPlan().catch((e) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).status).toBe(500);
    expect((err as HTTPError).message).toBe('Failed to load stack file: yaml: line 3: mapping values are not allowed');
  });

  it('surfaces the server error when no stack is deployed', async () => {
    mockResponse(503, { error: 'No stack is currently deployed' });

    const err = await fetchStackPlan().catch((e) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).status).toBe(503);
    expect((err as HTTPError).message).toBe('No stack is currently deployed');
  });

  it('falls back to the status text for a plan failure without an error string', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(new Response('oops', { status: 502, statusText: 'Bad Gateway' })),
    );

    const err = await fetchStackPlan().catch((e) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect((err as HTTPError).message).toBe('API error: 502 Bad Gateway');
  });

  it('returns the plan including override fields', async () => {
    const plan = {
      hasChanges: true,
      items: [{ action: 'change', kind: 'mcp-server', name: 'github', details: ['image changed'], override: true }],
      summary: '1 to change',
      override: '/tmp/stack.override.yaml',
    };
    mockResponse(200, plan);

    await expect(fetchStackPlan()).resolves.toEqual(plan);
  });
});
