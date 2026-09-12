import { useEffect } from 'react';
import { gatewayRequest } from '../lib/gatewayRequest';
import { useAuthStore } from '../stores/useAuthStore';

// The legacy endpoint negotiates an MCP endpoint and then closes. EOF does
// not signal shutdown; polling remains the disconnect fallback.
export function useSSEShutdown(_onShutdown: () => void) {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  useEffect(() => {
    if (authRequired) return;
    const controller = new AbortController();
    let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
    void (async () => {
      try {
        const response = await gatewayRequest('/sse', { headers: { Accept: 'text/event-stream' }, signal: controller.signal });
        if (!response.ok || controller.signal.aborted || useAuthStore.getState().generation !== generation) {
          await response.body?.cancel();
          return;
        }
        reader = response.body?.getReader();
        if (reader) {
          while (!controller.signal.aborted && useAuthStore.getState().generation === generation) {
            const { done } = await reader.read();
            if (done) break;
          }
        }
      } catch {
        // Shared policy handles gateway rejection. Other failures are left to
        // status polling; negotiation failures are not shutdown evidence.
      } finally {
        reader?.releaseLock();
      }
    })();
    return () => {
      controller.abort();
      void reader?.cancel().catch(() => { /* Abort may have already closed it. */ });
    };
  }, [generation, authRequired]);
}
