import { useAuthStore } from '../stores/useAuthStore';
import { credentialHeaders, type GatewayCredential } from './credentials';

export class AuthError extends Error {
  constructor(message = 'Gateway credential rejected. Verify your credential to resume.') {
    super(message);
    this.name = 'AuthError';
  }
}

export class GatewayRequestError extends Error {
  kind: 'origin' | 'redirect' | 'connection' | 'abort' | 'stale' | 'malformed';
  constructor(kind: GatewayRequestError['kind'], message: string) {
    super(message);
    this.name = 'GatewayRequestError';
    this.kind = kind;
  }
}

export async function gatewayRequest(input: string, init: RequestInit = {}, candidate?: GatewayCredential): Promise<Response> {
  const url = new URL(input, window.location.origin);
  if ((url.protocol !== 'http:' && url.protocol !== 'https:') || url.origin !== window.location.origin || url.username || url.password) {
    throw new GatewayRequestError('origin', 'Credential-bearing requests must use the gateway origin.');
  }
  const state = useAuthStore.getState();
  const generation = state.generation;
  if (!candidate && state.authRequired) throw new GatewayRequestError('stale', 'Protected requests are paused pending credential verification.');
  const controller = new AbortController();
  const unsubscribe = useAuthStore.subscribe(next => {
    if (!candidate && next.generation !== generation) controller.abort();
  });
  const signal = init.signal ? AbortSignal.any([init.signal, controller.signal]) : controller.signal;
  const current = () => {
    if (!candidate && useAuthStore.getState().generation !== generation) throw new GatewayRequestError('stale', 'Superseded credential request.');
  };
  try {
    let response: Response;
    const headers = credentialHeaders(candidate ?? state.credential, init.headers);
    try {
      response = await globalThis.fetch(url.href, { ...init, headers, signal, redirect: 'manual' });
    } catch {
      current();
      if (signal.aborted) throw new GatewayRequestError('abort', 'Gateway request cancelled.');
      throw new GatewayRequestError('connection', 'Gateway connection failed. Check connectivity, TLS, and browser access policy.');
    }
    current();
    if (response.type === 'opaqueredirect' || (response.status >= 300 && response.status < 400)) {
      throw new GatewayRequestError('redirect', 'Gateway redirect refused. Credentials were not forwarded.');
    }
    if (response.status === 401 && response.headers.get('Gridctl-Auth-Rejected') === '1') {
      if (!candidate) useAuthStore.getState().rejectGeneration(generation);
      throw new AuthError();
    }
    if (response.body) {
      const reader = response.body.getReader();
      const body = new ReadableStream<Uint8Array>({
        async pull(output) {
          const stop = useAuthStore.subscribe(next => {
            if (!candidate && next.generation !== generation) controller.abort();
          });
          try {
            current();
            const chunk = await reader.read();
            current();
            if (chunk.done) output.close();
            else output.enqueue(chunk.value);
          } catch (error) {
            await reader.cancel().catch(() => {});
            output.error(error instanceof GatewayRequestError ? error : new GatewayRequestError(signal.aborted ? 'abort' : 'connection', signal.aborted ? 'Gateway request cancelled.' : 'Gateway stream connection failed.'));
          } finally {
            stop();
          }
        },
        async cancel() { await reader.cancel(); },
      });
      response = new Response(body, { status: response.status, statusText: response.statusText, headers: response.headers });
    }
    // Guard consumption too: credentials can change after response headers.
    const json = response.json.bind(response);
    response.json = async () => {
      let result: unknown;
      try { result = await json(); } catch (error) {
        current();
        if (error instanceof GatewayRequestError) throw error;
        throw new GatewayRequestError('malformed', 'Gateway returned an invalid JSON response.');
      }
      current();
      return result;
    };
    return response;
  } finally {
    unsubscribe();
  }
}

export async function verifyCredential(candidate: GatewayCredential, signal?: AbortSignal): Promise<void> {
  const response = await gatewayRequest('/api/status', { signal }, candidate);
  if (!response.ok) throw new Error(`Gateway verification failed (HTTP ${response.status}).`);
  const body = await response.json();
  if (!body || typeof body.gateway?.name !== 'string' || typeof body.gateway?.version !== 'string' || (body['mcp-servers'] !== null && !Array.isArray(body['mcp-servers']))) {
    throw new GatewayRequestError('malformed', 'Gateway status response has an unexpected shape.');
  }
}
