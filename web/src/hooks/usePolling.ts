import { useCallback, useEffect, useRef } from 'react';
import { useNavigate } from 'react-router';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { usePinsStore, countFindingServers, firstDriftedServer, firstFindingsServer, countDriftedSkills, countFindingSkills, firstDriftedSkill, firstFindingsSkill, serverPinsChanged } from '../stores/usePinsStore';
import { useTelemetryStore } from '../stores/useTelemetryStore';
import { useSpecStore } from '../stores/useSpecStore';
import { useUIStore } from '../stores/useUIStore';
import { fetchStatus, fetchTools, fetchToolCatalog, fetchClients, fetchRegistryStatus, fetchRegistrySkills, fetchSkillSources, fetchServerPins, fetchSkillPins, fetchStackSpec, fetchStackHealth, getTelemetryInventory, AuthError, isFeatureUnavailable } from '../lib/api';
import { showToast } from '../components/ui/Toast';
import { POLLING } from '../lib/constants';
import { skillPinsUrl } from '../lib/skillGovernance';
import { GatewayRequestError } from '../lib/gatewayRequest';
import type { GatewayStatus } from '../types';
import { useBroadcastChannel, type BroadcastMessage } from './useBroadcastChannel';
import { useInterval } from './useInterval';
import { usePageVisible } from './usePageVisible';

let _prevDriftCount = 0;
let _prevFindingCount = 0;
let _prevSkillDriftCount = 0;
let _prevSkillFindingCount = 0;

// The shell is the only scheduler. Stack and the retry banners call through
// this slot; unmount clears it so a late Retry is a no-op.
let refreshImpl: (() => Promise<void>) | null = null;

// Last successful status object, before setGatewayStatus renames wire keys.
// Popout snapshots are this object, not a reconstruction from the store.
const lastStatusRef: { current: GatewayStatus | null } = { current: null };

export function requestGatewayPoll(): void {
  void refreshImpl?.();
}

export function usePollingRefresh(): () => Promise<void> {
  return useCallback(() => refreshImpl?.() ?? Promise.resolve(), []);
}

function failureMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Unknown error';
}

function isConnectionFailure(err: unknown): boolean {
  return err instanceof GatewayRequestError && err.kind === 'connection';
}

function pageVisible(): boolean {
  return typeof document === 'undefined' || document.visibilityState === 'visible';
}

export function usePolling(_options?: { registerRetry?: boolean }) {
  const navigate = useNavigate();
  const startedRef = useRef(false);
  const slowRunning = useRef(false);
  const seenVisible = useRef<boolean | null>(null);
  const seenWorkspace = useRef<string | null>(null);
  const postRef = useRef<(message: Omit<BroadcastMessage, 'timestamp'>) => void>(() => {});

  const setGatewayStatus = useStackStore((s) => s.setGatewayStatus);
  const setClients = useStackStore((s) => s.setClients);
  const setTools = useStackStore((s) => s.setTools);
  const setToolCatalog = useStackStore((s) => s.setToolCatalog);
  const setError = useStackStore((s) => s.setError);
  const setLoading = useStackStore((s) => s.setLoading);
  const setConnectionStatus = useStackStore((s) => s.setConnectionStatus);
  const refreshNodesAndEdges = useStackStore((s) => s.refreshNodesAndEdges);
  const authRequired = useAuthStore((s) => s.authRequired);
  const setAuthRequired = useAuthStore((s) => s.setAuthRequired);
  const activeWorkspace = useUIStore((s) => s.activeWorkspace);
  const visible = usePageVisible();

  const onChannelMessage = useCallback((msg: BroadcastMessage) => {
    if (msg.type !== 'REQUEST_STATE' || msg.source !== 'detached') return;
    if (!pageVisible()) return;
    const status = lastStatusRef.current;
    if (!status) return;
    postRef.current({
      type: 'STATUS_SNAPSHOT',
      source: 'main',
      payload: { status, at: Date.now() },
    });
  }, []);

  const { postMessage } = useBroadcastChannel({ onMessage: onChannelMessage });

  useEffect(() => {
    postRef.current = postMessage;
  }, [postMessage]);

  const runFast = useCallback(async () => {
    const generation = useAuthStore.getState().generation;
    const current = () => useAuthStore.getState().generation === generation && !useAuthStore.getState().authRequired;
    if (!current()) return;
    if (!startedRef.current) {
      startedRef.current = true;
      setLoading(true);
      setConnectionStatus('connecting');
    }
    try {
      const [status, toolsResult] = await Promise.all([
        fetchStatus(),
        fetchTools(),
      ]);
      if (!current()) return;

      lastStatusRef.current = status;
      setGatewayStatus(status);
      setTools(toolsResult.tools ?? []);
      setAuthRequired(false);
      if (pageVisible()) {
        postRef.current({
          type: 'STATUS_SNAPSHOT',
          source: 'main',
          payload: { status, at: Date.now() },
        });
      }

      try {
        const clients = await fetchClients();
        if (!current()) return;
        setClients(clients);
      } catch {
        // Client endpoint may not be available; ignore gracefully
      }
    } catch (error) {
      if (!current()) return;
      if (error instanceof AuthError) {
        setAuthRequired(true);
        setLoading(false);
        return;
      }

      // Pins and registry live on the slow cycle. Record the same failure
      // here so those surfaces do not spin while the gateway is down.
      const message = isConnectionFailure(error)
        ? 'Gateway connection unavailable'
        : failureMessage(error);
      setError(message);
      setConnectionStatus(isConnectionFailure(error) ? 'disconnected' : 'error');
      if (!current()) return;
      usePinsStore.getState().setPinsError(message);
      usePinsStore.getState().setSkillPinsError(message);
      useRegistryStore.getState().setError(message);
    }
  }, [setGatewayStatus, setClients, setTools, setError, setConnectionStatus, setAuthRequired, setLoading]);

  const runSlow = useCallback(async () => {
    if (slowRunning.current) return;
    slowRunning.current = true;
    const generation = useAuthStore.getState().generation;
    const current = () => useAuthStore.getState().generation === generation && !useAuthStore.getState().authRequired;
    try {
      if (!current()) return;

      try {
        const catalogResult = await fetchToolCatalog();
        if (!current()) return;
        setToolCatalog(catalogResult.tools ?? []);
      } catch {
        // Catalog is progressive disclosure; a miss leaves the prior list.
      }

      try {
        const prevPins = usePinsStore.getState().pins;
        const pins = await fetchServerPins();
        if (!current()) return;
        usePinsStore.getState().setPins(pins);
        if (serverPinsChanged(prevPins, pins)) {
          useStackStore.getState().refreshNodesAndEdges();
        }

        const driftedCount = Object.values(pins).filter((sp) => sp.status === 'drift').length;
        if (driftedCount > 0 && _prevDriftCount === 0) {
          const drifted = firstDriftedServer(pins);
          const target = drifted
            ? `/pins?server=${encodeURIComponent(drifted)}&view=drift`
            : '/pins';
          showToast('warning', `Schema drift detected on ${driftedCount} server${driftedCount > 1 ? 's' : ''}`, {
            action: { label: 'View', onClick: () => navigate(target) },
            duration: 6000,
          });
        }
        _prevDriftCount = driftedCount;

        const findingCount = countFindingServers(pins);
        if (findingCount > 0 && _prevFindingCount === 0) {
          const flagged = firstFindingsServer(pins);
          const target = flagged
            ? `/pins?server=${encodeURIComponent(flagged)}&view=findings`
            : '/pins';
          showToast('warning', `Poisoning-scan findings on ${findingCount} server${findingCount > 1 ? 's' : ''}`, {
            action: { label: 'View', onClick: () => navigate(target) },
            duration: 6000,
          });
        }
        _prevFindingCount = findingCount;
      } catch (err) {
        if (!current()) return;
        if (isFeatureUnavailable(err)) {
          if (usePinsStore.getState().pins === null) {
            usePinsStore.getState().setPins({});
          } else {
            usePinsStore.getState().setPinsError(null);
          }
        } else {
          usePinsStore.getState().setPinsError(
            isConnectionFailure(err) ? 'Gateway connection unavailable' : failureMessage(err),
          );
        }
      }

      try {
        const skillPins = await fetchSkillPins();
        if (!current()) return;
        usePinsStore.getState().setSkillPins(skillPins);

        const skillDriftCount = countDriftedSkills(skillPins);
        if (skillDriftCount > 0 && _prevSkillDriftCount === 0) {
          const drifted = firstDriftedSkill(skillPins);
          const target = skillPinsUrl(drifted, 'drift');
          showToast('warning', `Pin drift detected on ${skillDriftCount} skill${skillDriftCount > 1 ? 's' : ''}`, {
            action: { label: 'View', onClick: () => navigate(target) },
            duration: 6000,
          });
        }
        _prevSkillDriftCount = skillDriftCount;

        const skillFindingCount = countFindingSkills(skillPins);
        if (skillFindingCount > 0 && _prevSkillFindingCount === 0) {
          const flagged = firstFindingsSkill(skillPins);
          const target = skillPinsUrl(flagged, 'findings');
          showToast('warning', `Advisory findings on ${skillFindingCount} skill${skillFindingCount > 1 ? 's' : ''}`, {
            action: { label: 'View', onClick: () => navigate(target) },
            duration: 6000,
          });
        }
        _prevSkillFindingCount = skillFindingCount;
      } catch (err) {
        if (!current()) return;
        if (isFeatureUnavailable(err)) {
          if (usePinsStore.getState().skillPins === null) {
            usePinsStore.getState().setSkillPins({});
          } else {
            usePinsStore.getState().setSkillPinsError(null);
          }
        } else {
          usePinsStore.getState().setSkillPinsError(
            isConnectionFailure(err) ? 'Gateway connection unavailable' : failureMessage(err),
          );
        }
      }

      try {
        const records = await getTelemetryInventory();
        if (!current()) return;
        useTelemetryStore.getState().setInventory(records);
      } catch {
        // Telemetry endpoint may be unreachable; leave the prior snapshot.
      }
      try {
        const spec = await fetchStackSpec();
        if (!current()) return;
        useTelemetryStore.getState().setRawSpec(spec.content);
      } catch {
        if (current()) useTelemetryStore.getState().setRawSpec(null);
      }

      try {
        const health = await fetchStackHealth();
        if (!current()) return;
        useSpecStore.getState().setHealth(health);
      } catch {
        // Spec health is a badge, not a global error.
      }

      try {
        const [regStatus, regSkills] = await Promise.all([
          fetchRegistryStatus(),
          fetchRegistrySkills(),
        ]);
        if (!current()) return;
        const prevSkills = useRegistryStore.getState().skills;
        useRegistryStore.getState().setStatus(regStatus);
        useRegistryStore.getState().setSkills(regSkills);
        useRegistryStore.getState().setError(null);

        const prevActiveCount = (prevSkills ?? []).filter((s) => s.state === 'active').length;
        const nextActiveCount = regSkills.filter((s) => s.state === 'active').length;
        if (prevActiveCount !== nextActiveCount) {
          refreshNodesAndEdges();
        }
      } catch (err) {
        if (!current()) return;
        if (isFeatureUnavailable(err)) {
          useRegistryStore.getState().setSkills([]);
          useRegistryStore.getState().setError(null);
        } else {
          useRegistryStore.getState().setError(
            isConnectionFailure(err) ? 'Gateway connection unavailable' : failureMessage(err),
          );
        }
      }

      try {
        const sources = await fetchSkillSources();
        if (!current()) return;
        useRegistryStore.getState().setSources(sources);
      } catch {
        // Sources unavailable — progressive disclosure, not an error.
      }
    } finally {
      slowRunning.current = false;
    }
  }, [navigate, refreshNodesAndEdges, setToolCatalog]);

  const refresh = useCallback(async () => {
    await Promise.all([runFast(), runSlow()]);
  }, [runFast, runSlow]);

  useEffect(() => {
    refreshImpl = refresh;
    return () => {
      if (refreshImpl === refresh) refreshImpl = null;
    };
  }, [refresh]);

  useEffect(() => {
    if (authRequired) startedRef.current = false;
  }, [authRequired]);

  useInterval(runFast, POLLING.STATUS, {
    enabled: !authRequired,
    hiddenIntervalMs: POLLING.STATUS_HIDDEN,
  });

  useInterval(runSlow, POLLING.INVENTORY, {
    enabled: !authRequired,
  });

  // Inventory catches up on return even when the last run is younger than
  // POLLING.INVENTORY. The in-flight guard collapses a coinciding tick.
  useEffect(() => {
    if (seenVisible.current === null) {
      seenVisible.current = visible;
      return;
    }
    const wasVisible = seenVisible.current;
    seenVisible.current = visible;
    if (!wasVisible && visible && !useAuthStore.getState().authRequired) {
      void runSlow();
    }
  }, [visible, runSlow]);

  useEffect(() => {
    if (seenWorkspace.current === null) {
      seenWorkspace.current = activeWorkspace;
      return;
    }
    if (seenWorkspace.current === activeWorkspace) return;
    seenWorkspace.current = activeWorkspace;
    if (!useAuthStore.getState().authRequired) void runSlow();
  }, [activeWorkspace, runSlow]);

  return { refresh };
}
