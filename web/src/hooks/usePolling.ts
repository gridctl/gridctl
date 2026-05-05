import { useEffect, useRef, useCallback } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { usePinsStore } from '../stores/usePinsStore';
import { useUIStore } from '../stores/useUIStore';
import { useTelemetryStore } from '../stores/useTelemetryStore';
import { fetchStatus, fetchTools, fetchClients, fetchRegistryStatus, fetchRegistrySkills, fetchServerPins, fetchStackSpec, getTelemetryInventory, AuthError } from '../lib/api';
import { showToast } from '../components/ui/Toast';
import { POLLING } from '../lib/constants';

let _prevDriftCount = 0;

export function usePolling() {
  const intervalRef = useRef<number | null>(null);

  const setGatewayStatus = useStackStore((s) => s.setGatewayStatus);
  const setClients = useStackStore((s) => s.setClients);
  const setTools = useStackStore((s) => s.setTools);
  const setError = useStackStore((s) => s.setError);
  const setLoading = useStackStore((s) => s.setLoading);
  const setConnectionStatus = useStackStore((s) => s.setConnectionStatus);
  const refreshNodesAndEdges = useStackStore((s) => s.refreshNodesAndEdges);

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

      // Fetch pins — progressive disclosure, never blocks main cycle
      try {
        const pins = await fetchServerPins();
        usePinsStore.getState().setPins(pins);
        // Re-apply pin state to nodes built from the status fetch above
        useStackStore.getState().refreshNodesAndEdges();

        const driftedCount = Object.values(pins).filter((sp) => sp.status === 'drift').length;
        if (driftedCount > 0 && _prevDriftCount === 0) {
          showToast('warning', `Schema drift detected on ${driftedCount} server${driftedCount > 1 ? 's' : ''}`, {
            action: { label: 'View', onClick: () => useUIStore.getState().setBottomPanelTab('pins') },
            duration: 6000,
          });
        }
        _prevDriftCount = driftedCount;
      } catch {
        // Pins endpoint unavailable (feature not enabled) — suppress silently
      }

      // Fetch telemetry inventory + stack spec — progressive disclosure.
      // Inventory drives the header pill / wipe modal / graph dot; the
      // raw spec is parsed client-side to derive the per-server overrides
      // the tri-state controls need. Both endpoints fail closed: an
      // unrelated API error (or stackless mode) collapses to empty data
      // without surfacing a global error.
      try {
        const records = await getTelemetryInventory();
        useTelemetryStore.getState().setInventory(records);
      } catch {
        // Telemetry endpoint may be unreachable (stackless mode, older
        // daemon); leave the prior snapshot in place silently.
      }
      try {
        const spec = await fetchStackSpec();
        useTelemetryStore.getState().setRawSpec(spec.content);
      } catch {
        // Stackless mode returns 503; clear so the UI does not show
        // stale telemetry config from a previous stack.
        useTelemetryStore.getState().setRawSpec(null);
      }

      // Fetch registry data — progressive disclosure, never blocks main cycle
      try {
        const [regStatus, regSkills] = await Promise.all([
          fetchRegistryStatus(),
          fetchRegistrySkills(),
        ]);
        const prevSkills = useRegistryStore.getState().skills;
        useRegistryStore.getState().setStatus(regStatus);
        useRegistryStore.getState().setSkills(regSkills);
        useRegistryStore.getState().setError(null);

        // Refresh graph when skill count changes so skill nodes appear/disappear
        const prevActiveCount = (prevSkills ?? []).filter((s) => s.state === 'active').length;
        const nextActiveCount = regSkills.filter((s) => s.state === 'active').length;
        if (prevActiveCount !== nextActiveCount) {
          refreshNodesAndEdges();
        }
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
  }, [setGatewayStatus, setClients, setTools, setError, setConnectionStatus, setAuthRequired, setLoading, refreshNodesAndEdges]);

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
