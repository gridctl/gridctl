import { useEffect, useRef, useCallback } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { fetchStatus, fetchTools } from '../lib/api';
import { POLLING } from '../lib/constants';

export function usePolling() {
  const intervalRef = useRef<number | null>(null);

  const setGatewayStatus = useStackStore((s) => s.setGatewayStatus);
  const setTools = useStackStore((s) => s.setTools);
  const setError = useStackStore((s) => s.setError);
  const setLoading = useStackStore((s) => s.setLoading);
  const setConnectionStatus = useStackStore((s) => s.setConnectionStatus);

  const poll = useCallback(async () => {
    try {
      const [status, toolsResult] = await Promise.all([
        fetchStatus(),
        fetchTools(),
      ]);

      setGatewayStatus(status);
      setTools(toolsResult.tools);
    } catch (error) {
      setError(error instanceof Error ? error.message : 'Unknown error');
      setConnectionStatus('error');
    }
  }, [setGatewayStatus, setTools, setError, setConnectionStatus]);

  useEffect(() => {
    // Initial fetch
    setLoading(true);
    setConnectionStatus('connecting');
    poll();

    // Set up polling interval
    intervalRef.current = window.setInterval(poll, POLLING.STATUS);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [poll, setLoading, setConnectionStatus]);

  // Manual refresh function
  const refresh = useCallback(() => {
    poll();
  }, [poll]);

  return { refresh };
}
