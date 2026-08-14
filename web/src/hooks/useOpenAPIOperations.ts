import { useCallback, useRef, useState } from 'react';
import {
  previewOpenAPIOperations,
  type OpenAPIOperation,
  type OpenAPIPreviewRequest,
  type OpenAPIPreviewSuccess,
  type ProbeError,
} from '../lib/api';
import { getWizardSessionId } from '../lib/wizardSession';

export interface OpenAPIOperationsState {
  loading: boolean;
  error: ProbeError | Error | null;
  operations: OpenAPIOperation[] | null;
  skippedCount: number;
  loadedAt: string | null;
  cached: boolean;
  // The spec string that produced the current result, so the picker can tell
  // the operator their edit is not yet reflected in the list below.
  loadedSpec: string | null;
}

export interface UseOpenAPIOperations extends OpenAPIOperationsState {
  load: (request: OpenAPIPreviewRequest) => Promise<OpenAPIPreviewSuccess | null>;
  reset: () => void;
}

const initialState: OpenAPIOperationsState = {
  loading: false,
  error: null,
  operations: null,
  skippedCount: 0,
  loadedAt: null,
  cached: false,
  loadedSpec: null,
};

/**
 * useOpenAPIOperations wraps POST /api/openapi/operations in a small state
 * machine, mirroring useProbeServer: a new load() aborts any in-flight one, so
 * the last request wins when the operator edits the spec and clicks again.
 *
 * Loading is always explicit. The spec field never triggers a fetch on blur —
 * parsing a multi-megabyte document is expensive on the daemon and the operator
 * may be midway through typing a URL.
 */
export function useOpenAPIOperations(): UseOpenAPIOperations {
  const [state, setState] = useState<OpenAPIOperationsState>(initialState);
  const inFlight = useRef<AbortController | null>(null);

  const reset = useCallback(() => {
    inFlight.current?.abort();
    inFlight.current = null;
    setState(initialState);
  }, []);

  const load = useCallback(
    async (request: OpenAPIPreviewRequest): Promise<OpenAPIPreviewSuccess | null> => {
      inFlight.current?.abort();
      const controller = new AbortController();
      inFlight.current = controller;

      setState((s) => ({ ...s, loading: true, error: null }));
      try {
        const result = await previewOpenAPIOperations(
          request,
          getWizardSessionId(),
          controller.signal,
        );
        if (controller.signal.aborted) return null;
        setState({
          loading: false,
          error: null,
          operations: result.operations ?? [],
          skippedCount: result.skipped_count ?? 0,
          loadedAt: result.loaded_at,
          cached: result.cached,
          loadedSpec: request.spec,
        });
        return result;
      } catch (err) {
        if (controller.signal.aborted) return null;
        // Keep any previously loaded list. A failed reload should surface the
        // error without throwing away a working result the operator is midway
        // through selecting from.
        setState((s) => ({
          ...s,
          loading: false,
          cached: false,
          error: err instanceof Error ? err : new Error(String(err)),
        }));
        return null;
      } finally {
        if (inFlight.current === controller) {
          inFlight.current = null;
        }
      }
    },
    [],
  );

  return { ...state, load, reset };
}
