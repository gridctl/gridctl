import { useCallback, useRef, useState } from 'react';
import {
  probeServer,
  ProbeError,
  type ProbeServerConfig,
  type ProbeSuccess,
  type ProbedTool,
} from '../lib/api';

// A stable session id for this tab, sent as X-Session-ID so the backend can
// enforce per-session concurrency caps separately from global ones. Generating
// it lazily the first time the hook is used avoids wasting entropy for users
// who never open the wizard.
let sessionId: string | null = null;
function getSessionId(): string {
  if (sessionId) return sessionId;
  sessionId = `wizard-${Math.random().toString(36).slice(2, 10)}-${Date.now()}`;
  return sessionId;
}

export interface ProbeState {
  loading: boolean;
  error: ProbeError | Error | null;
  tools: ProbedTool[] | null;
  probedAt: string | null;
  cached: boolean;
}

export interface UseProbeServer extends ProbeState {
  probe: (config: ProbeServerConfig) => Promise<ProbeSuccess | null>;
  reset: () => void;
}

const initialState: ProbeState = {
  loading: false,
  error: null,
  tools: null,
  probedAt: null,
  cached: false,
};

/**
 * useProbeServer wraps the /api/servers/probe call in a small state machine
 * so components can render loading / error / success without juggling fetch
 * plumbing. A new probe() call cancels any previous in-flight probe — the
 * last request wins, which matches user expectations when the config changes
 * between clicks.
 */
export function useProbeServer(): UseProbeServer {
  const [state, setState] = useState<ProbeState>(initialState);
  const inFlight = useRef<AbortController | null>(null);

  const reset = useCallback(() => {
    inFlight.current?.abort();
    inFlight.current = null;
    setState(initialState);
  }, []);

  const probe = useCallback(async (config: ProbeServerConfig): Promise<ProbeSuccess | null> => {
    inFlight.current?.abort();
    const controller = new AbortController();
    inFlight.current = controller;

    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      const result = await probeServer(config, getSessionId());
      if (controller.signal.aborted) return null;
      setState({
        loading: false,
        error: null,
        tools: result.tools,
        probedAt: result.probedAt,
        cached: result.cached,
      });
      return result;
    } catch (err) {
      if (controller.signal.aborted) return null;
      setState({
        loading: false,
        error: err instanceof Error ? err : new Error(String(err)),
        tools: null,
        probedAt: null,
        cached: false,
      });
      return null;
    } finally {
      if (inFlight.current === controller) {
        inFlight.current = null;
      }
    }
  }, []);

  return { ...state, probe, reset };
}
