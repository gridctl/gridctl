import { useEffect, useRef, useCallback } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { fetchStatus, fetchTools, AuthError } from '../lib/api';
import { POLLING } from '../lib/constants';

export function usePolling() {
  const intervalRef = useRef<number | null>(null);

  const setGatewayStatus = useStackStore((s) => s.setGatewayStatus);
  const setTools = useStackStore((s) => s.setTools);
  const setError = useStackStore((s) => s.setError);
  const setLoading = useStackStore((s) => s.setLoading);
  const setConnectionStatus = useStackStore((s) => s.setConnectionStatus);

  const authRequired = useAuthStore((s) => s.authRequired);
  const setAuthRequired = useAuthStore((s) => s.setAuthRequired);

  const poll = useCallback(async () => {
    try {
      const [status, toolsResult] = await Promise.all([
        fetchStatus(),
        fetchTools(),
      ]);

      setGatewayStatus(status);
      setTools(toolsResult.tools);
      setAuthRequired(false);
    } catch (error) {
      if (error instanceof AuthError) {
        setAuthRequired(true);
        setLoading(false);
        return;
      }
      setError(error instanceof Error ? error.message : 'Unknown error');
      setConnectionStatus('error');
    }
  }, [setGatewayStatus, setTools, setError, setConnectionStatus, setAuthRequired, setLoading]);

  useEffect(() => {
    // Don't poll while auth prompt is showing
    if (authRequired) {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }

    // Initial fetch
    setLoading(true);
    setConnectionStatus('connecting');
    poll();

    // Set up polling interval
    intervalRef.current = window.setInterval(poll, POLLING.STATUS);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [poll, setLoading, setConnectionStatus, authRequired]);

  // Manual refresh function
  const refresh = useCallback(() => {
    poll();
  }, [poll]);

  return { refresh };
}
