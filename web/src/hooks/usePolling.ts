import { useEffect, useRef, useCallback } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { fetchStatus, fetchTools, fetchClients, fetchRegistryStatus, fetchRegistryPrompts, fetchRegistrySkills, AuthError } from '../lib/api';
import { POLLING } from '../lib/constants';

export function usePolling() {
  const intervalRef = useRef<number | null>(null);

  const setGatewayStatus = useStackStore((s) => s.setGatewayStatus);
  const setClients = useStackStore((s) => s.setClients);
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

      // Fetch clients separately — failure should not block core updates
      try {
        const clients = await fetchClients();
        setClients(clients);
      } catch {
        // Client endpoint may not be available; ignore gracefully
      }

      // Fetch registry data — progressive disclosure, never blocks main cycle
      try {
        const [regStatus, regPrompts, regSkills] = await Promise.all([
          fetchRegistryStatus(),
          fetchRegistryPrompts(),
          fetchRegistrySkills(),
        ]);
        useRegistryStore.getState().setStatus(regStatus);
        useRegistryStore.getState().setPrompts(regPrompts);
        useRegistryStore.getState().setSkills(regSkills);
        useRegistryStore.getState().setError(null);
      } catch {
        // Registry not available — not an error (progressive disclosure)
      }
    } catch (error) {
      if (error instanceof AuthError) {
        setAuthRequired(true);
        setLoading(false);
        return;
      }

      // Differentiate network errors from HTTP errors
      if (error instanceof TypeError && error.message === 'Failed to fetch') {
        // Network error: gateway unreachable (shutdown, crash, or network issue)
        setError('Gateway unavailable — connection refused');
        setConnectionStatus('disconnected');
      } else {
        // HTTP error: gateway is running but returned an error
        setError(error instanceof Error ? error.message : 'Unknown error');
        setConnectionStatus('error');
      }
    }
  }, [setGatewayStatus, setClients, setTools, setError, setConnectionStatus, setAuthRequired, setLoading]);

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
