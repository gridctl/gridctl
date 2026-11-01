import { useAuthStore } from '../stores/useAuthStore';
import { credentialHeaders, type GatewayCredential } from './credentials';

export class AuthError extends Error {
  constructor(message = 'Gateway credential rejected. Verify your credential to resume.') {
    super(message);
    this.name = 'AuthError';
  }
}

export const credentialRejectedMessage = 'The gateway rejected this credential. Check the mode, header, and token against the running stack\'s gateway.auth block.';

export class VerificationError extends Error {
  kind: 'rejected' | 'forbidden' | 'gateway_error' | 'unexpected';
  status: number;
  constructor(kind: VerificationError['kind'], status: number, message: string) {
    super(message);
    this.name = 'VerificationError';
    this.kind = kind;
    this.status = status;
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

function verificationError(status: number): VerificationError {
  if (status === 401) return new VerificationError('rejected', status, credentialRejectedMessage);
  if (status === 403) {
    return new VerificationError('forbidden', status, 'The gateway refused this browser\'s Host header, not the credential. Open the UI through an allowed host, or add this host to gateway.allowed_hosts.');
  }
  if (status >= 500 && status <= 599) {
    return new VerificationError('gateway_error', status, `The gateway returned a server error (HTTP ${status}). This is not a credential problem. Check gridctl logs and retry.`);
  }
  return new VerificationError('unexpected', status, `Gateway verification failed (HTTP ${status}).`);
}

export async function verifyCredential(candidate: GatewayCredential, signal?: AbortSignal): Promise<void> {
  const response = await gatewayRequest('/api/status', { signal }, candidate);
  if (!response.ok) throw verificationError(response.status);
  const body = await response.json();
  if (!body || typeof body.gateway?.name !== 'string' || typeof body.gateway?.version !== 'string' || (body['mcp-servers'] !== null && !Array.isArray(body['mcp-servers']))) {
    throw new GatewayRequestError('malformed', 'Gateway status response has an unexpected shape.');
  }
}
