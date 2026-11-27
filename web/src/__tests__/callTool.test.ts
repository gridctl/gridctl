import { afterEach, describe, expect, it, vi } from 'vitest';
import { callTool, ToolCallError, TOOL_CALL_MAX_BYTES } from '../lib/api';
import { GatewayRequestError } from '../lib/gatewayRequest';

afterEach(() => {
  vi.unstubAllGlobals();
});

const OK = {
  schema_version: 1,
  client: 'ui',
  name: 'echo__echo',
  outcome: { disposition: 'completed', stage: 'downstream', reason: 'ok', completion: 'complete' },
  result: { content: [{ type: 'text', text: 'hello' }] },
  error: null,
};

function jsonResponse(status: number, body: unknown) {
  return new Response(typeof body === 'string' ? body : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function stub(status: number, body: unknown) {
  const fetchMock = vi.fn().mockResolvedValue(jsonResponse(status, body));
  vi.stubGlobal('fetch', fetchMock);
  return fetchMock;
}

describe('callTool', () => {
  it('POSTs exactly name, arguments, and client ui', async () => {
    const fetchMock = stub(200, OK);
    const env = await callTool('echo__echo', { message: 'hi' });
    expect(env).toEqual(OK);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(new URL('/api/tools/call', window.location.origin).href);
    expect(init.method).toBe('POST');
    expect(new Headers(init.headers).get('Content-Type')).toBe('application/json');
    const body = JSON.parse(init.body);
    expect(body).toEqual({ name: 'echo__echo', arguments: { message: 'hi' }, client: 'ui' });
    expect(Object.keys(body).sort()).toEqual(['arguments', 'client', 'name']);
  });

  it('resolves a denial at 200 as an envelope', async () => {
    const denial = {
      ...OK,
      outcome: { disposition: 'denied', stage: 'gate', reason: 'policy_blocked', completion: 'not_started', gate: 'policy' },
      result: null,
      error: { code: 'policy_blocked', message: 'tool blocked by policy' },
    };
    stub(200, denial);
    await expect(callTool('echo__echo', {})).resolves.toEqual(denial);
  });

  it('resolves a tool error with its result', async () => {
    const toolError = {
      ...OK,
      outcome: { disposition: 'tool_error', stage: 'downstream', reason: 'tool_error', completion: 'complete' },
      result: { isError: true, content: [{ type: 'text', text: 'nope' }] },
      error: { code: 'tool_error', message: 'tool returned an error' },
    };
    stub(200, toolError);
    const env = await callTool('echo__echo', {});
    expect(env.result?.isError).toBe(true);
  });

  it('keeps trace_id when present', async () => {
    stub(200, { ...OK, trace_id: 'abc123' });
    expect((await callTool('echo__echo', {})).trace_id).toBe('abc123');
  });

  it.each([400, 413, 415])('resolves a %i validation envelope', async (status) => {
    const env = {
      schema_version: 1,
      outcome: { disposition: 'invalid_request', stage: 'input', reason: 'invalid_name', completion: 'not_started' },
      result: null,
      error: { code: 'invalid_name', message: 'name must be server__tool with nonempty halves' },
    };
    stub(status, env);
    await expect(callTool('x', {})).resolves.toEqual(env);
  });

  it('reports a 503 envelope as gateway unavailable', async () => {
    stub(503, {
      schema_version: 1,
      outcome: { disposition: 'routing_failed', stage: 'daemon', reason: 'gateway_unavailable', completion: 'not_started' },
      result: null,
      error: { code: 'gateway_unavailable', message: 'gateway is unavailable' },
    });
    await expect(callTool('echo__echo', {})).rejects.toMatchObject({ name: 'ToolCallError', kind: 'unavailable' });
  });

  it('rejects a malformed 200 body', async () => {
    stub(200, 'not json');
    await expect(callTool('echo__echo', {})).rejects.toMatchObject({ kind: 'malformed' });
  });

  it('rejects a 200 body without a valid outcome', async () => {
    stub(200, { schema_version: 1, outcome: { disposition: 'completed' }, result: null, error: null });
    await expect(callTool('echo__echo', {})).rejects.toMatchObject({ kind: 'malformed' });
    stub(200, { ...OK, schema_version: 2 });
    await expect(callTool('echo__echo', {})).rejects.toMatchObject({ kind: 'malformed' });
  });

  it('reports other statuses and non-envelope bodies as a generic error with the status', async () => {
    stub(500, 'boom');
    await expect(callTool('echo__echo', {})).rejects.toMatchObject({ kind: 'http', status: 500 });
    stub(400, { error: 'legacy' });
    await expect(callTool('echo__echo', {})).rejects.toMatchObject({ kind: 'http', status: 400 });
  });

  it('propagates an abort as a GatewayRequestError', async () => {
    const fetchMock = vi.fn((_url: string, init: RequestInit) => new Promise((_resolve, reject) => {
      init.signal?.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
    }));
    vi.stubGlobal('fetch', fetchMock);
    const controller = new AbortController();
    const pending = callTool('echo__echo', {}, { signal: controller.signal });
    controller.abort();
    await expect(pending).rejects.toBeInstanceOf(GatewayRequestError);
    await expect(pending).rejects.toMatchObject({ kind: 'abort' });
    expect((fetchMock.mock.calls[0][1] as RequestInit).signal?.aborted).toBe(true);
  });

  it('refuses a body over the 2 MiB limit without sending', async () => {
    const fetchMock = stub(200, OK);
    await expect(callTool('echo__echo', { blob: 'x'.repeat(TOOL_CALL_MAX_BYTES) })).rejects.toBeInstanceOf(ToolCallError);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
