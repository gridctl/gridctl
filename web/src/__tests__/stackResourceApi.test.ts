import { afterEach, describe, expect, it, vi } from 'vitest';
import { appendToStack, HTTPError, StackAppendError, validateStackResource } from '../lib/api';

afterEach(() => {
  vi.unstubAllGlobals();
});

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
}

const unknownField = {
  valid: false,
  errorCount: 1,
  issues: [{ field: 'imagee', message: 'unknown field "imagee" (line 4)', severity: 'error', line: 4 }],
};

describe('validateStackResource', () => {
  it('resolves a 400 strict-decode body with issues as the validation result', async () => {
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(unknownField, 400));
    vi.stubGlobal('fetch', fetchMock);

    await expect(validateStackResource('name: x\nimagee: y\n', 'mcp-server')).resolves.toEqual(unknownField);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(new URL(url, window.location.origin).pathname).toBe('/api/stack/resource/validate');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body as string)).toEqual({ yaml: 'name: x\nimagee: y\n', resourceType: 'mcp-server' });
  });

  it('rejects a 400 without an issues array with HTTPError', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'Unsupported resourceType: x' }, 400)));

    const err = await validateStackResource('name: x\n', 'resource').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect(err).toEqual(expect.objectContaining({ status: 400, message: 'Unsupported resourceType: x' }));
  });

  it('rejects a 500 with HTTPError even when it carries issues', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(unknownField, 500)));

    const err = await validateStackResource('name: x\n', 'mcp-server').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect(err).toEqual(expect.objectContaining({ status: 500 }));
  });

  it('rejects a 401 with HTTPError status 401', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({ error: 'unauthorized' }, 401)));

    const err = await validateStackResource('name: x\n', 'mcp-server').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(HTTPError);
    expect(err).toEqual(expect.objectContaining({ status: 401 }));
  });
});

describe('appendToStack', () => {
  it('rejects a 422 post-append failure with StackAppendError carrying the issues', async () => {
    const issue = { field: 'mcp-servers[1].name', message: "duplicate MCP server name 'github'", severity: 'error' };
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse({
      error: 'stack validation failed after append',
      validation: { valid: false, errorCount: 1, issues: [issue] },
    }, 422)));

    const err = await appendToStack('name: github\n', 'mcp-server').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(StackAppendError);
    expect(err).toEqual(expect.objectContaining({
      status: 422,
      message: 'stack validation failed after append',
      issues: [issue],
    }));
  });

  it('rejects a 400 bare validation body with its issues and a readable message', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(unknownField, 400)));

    const err = await appendToStack('name: x\nimagee: y\n', 'mcp-server').catch((e: unknown) => e) as StackAppendError;
    expect(err).toBeInstanceOf(StackAppendError);
    expect(err.status).toBe(400);
    expect(err.issues).toEqual(unknownField.issues);
    expect(err.message).not.toBe('Deploy failed: 400');
    expect(err.message).toBe('The YAML was rejected');
  });

  it('does not surface a JSON parse error for a non-JSON error body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('upstream exploded', { status: 502 })));

    const err = await appendToStack('name: x\n', 'resource').catch((e: unknown) => e) as StackAppendError;
    expect(err).toBeInstanceOf(StackAppendError);
    expect(err.message).toBe('Deploy failed: 502');
    expect(err.issues).toEqual([]);
  });

  it('resolves the append result on success', async () => {
    const body = { success: true, resourceType: 'resource', resourceName: 'db' };
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(body, 200)));

    await expect(appendToStack('name: db\n', 'resource')).resolves.toEqual(body);
  });
});
