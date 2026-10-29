import { useEffect, useRef, useCallback } from 'react';
import { useNavigate } from 'react-router';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { usePinsStore, countFindingServers, firstDriftedServer, firstFindingsServer, countDriftedSkills, countFindingSkills, firstDriftedSkill, firstFindingsSkill, serverPinsChanged } from '../stores/usePinsStore';
import { useTelemetryStore } from '../stores/useTelemetryStore';
import { fetchStatus, fetchTools, fetchToolCatalog, fetchClients, fetchRegistryStatus, fetchRegistrySkills, fetchSkillSources, fetchServerPins, fetchSkillPins, fetchStackSpec, getTelemetryInventory, AuthError, isFeatureUnavailable } from '../lib/api';
import { showToast } from '../components/ui/Toast';
import { POLLING } from '../lib/constants';
import { skillPinsUrl } from '../lib/skillGovernance';
import { GatewayRequestError } from '../lib/gatewayRequest';

let _prevDriftCount = 0;
let _prevFindingCount = 0;
let _prevSkillDriftCount = 0;
let _prevSkillFindingCount = 0;

// Shell-only retry slot. A second usePolling instance must not overwrite or
// clear it, or leaving Stack would no-op Retry on Pins, Library, and Tools.
let registeredPoll: (() => void) | null = null;

// Re-issues the shell poll. No-op when no registering hook is mounted.
export function requestGatewayPoll(): void {
  registeredPoll?.();
}

function failureMessage(err: unknown): string {
  return err instanceof Error ? err.message : 'Unknown error';
}

export function usePolling(options?: { registerRetry?: boolean }) {
  const intervalRef = useRef<number | null>(null);
  const navigate = useNavigate();

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

  const poll = useCallback(async () => {
    const generation = useAuthStore.getState().generation;
    const current = () => useAuthStore.getState().generation === generation && !useAuthStore.getState().authRequired;
    if (!current()) return;
    try {
      const [status, toolsResult, catalogResult] = await Promise.all([
        fetchStatus(),
        fetchTools(),
        fetchToolCatalog(),
      ]);
      if (!current()) return;

      setGatewayStatus(status);
      // Coalesce to []: these endpoints serialize an empty inventory as null
      // (nil Go slice) in stackless mode, and downstream consumers index into
      // the catalog (e.g. useFuzzySearch → new Fuse) where null would throw.
      setTools(toolsResult.tools ?? []);
      setToolCatalog(catalogResult.tools ?? []);
      setAuthRequired(false);

      // Fetch clients separately — failure should not block core updates
      try {
        const clients = await fetchClients();
        if (!current()) return;
        setClients(clients);
      } catch {
        // Client endpoint may not be available; ignore gracefully
      }

      // Fetch pins — progressive disclosure, never blocks main cycle
      try {
        const prevPins = usePinsStore.getState().pins;
        const pins = await fetchServerPins();
        if (!current()) return;
        usePinsStore.getState().setPins(pins);
        // Re-apply pin annotation only when status, tool count, or the server
        // set changed. Client changes paint through setClients in this cycle.
        if (serverPinsChanged(prevPins, pins)) {
          useStackStore.getState().refreshNodesAndEdges();
        }

        const driftedCount = Object.values(pins).filter((sp) => sp.status === 'drift').length;
        if (driftedCount > 0 && _prevDriftCount === 0) {
          // Land directly on a drifted server's diff, where the Approve
          // action lives; the workspace validates the ?server= param.
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

        // Poisoning-scan findings fire their own rising-edge toast, guarded
        // by the same sentinel pattern as drift so it never repeats per poll.
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
        // 503 is defensive: disabled pinning is a 200 empty map. Leave the
        // loaded map in place (coalesce a first-load null so the empty state
        // can render) and do not record an error.
        if (!current()) return;
        if (isFeatureUnavailable(err)) {
          if (usePinsStore.getState().pins === null) {
            usePinsStore.getState().setPins({});
          } else {
            usePinsStore.getState().setPinsError(null);
          }
        } else {
          usePinsStore.getState().setPinsError(failureMessage(err));
        }
      }

      // Skill pins ride the same cadence in their own progressive-disclosure
      // block, with rising-edge toasts guarded like the server sentinels.
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
          usePinsStore.getState().setSkillPinsError(failureMessage(err));
        }
      }

      // Fetch telemetry inventory + stack spec — progressive disclosure.
      // Inventory drives the header pill / wipe modal / graph dot; the
      // raw spec is parsed client-side to derive the per-server overrides
      // the tri-state controls need. Both endpoints fail closed: an
      // unrelated API error (or stackless mode) collapses to empty data
      // without surfacing a global error.
      try {
        const records = await getTelemetryInventory();
        if (!current()) return;
        useTelemetryStore.getState().setInventory(records);
      } catch {
        // Telemetry endpoint may be unreachable (stackless mode, older
        // daemon); leave the prior snapshot in place silently.
      }
      try {
        const spec = await fetchStackSpec();
        if (!current()) return;
        useTelemetryStore.getState().setRawSpec(spec.content);
      } catch {
        // Stackless mode returns 503; clear so the UI does not show
        // stale telemetry config from a previous stack.
        if (current()) useTelemetryStore.getState().setRawSpec(null);
      }

      // Fetch registry data — progressive disclosure, never blocks main cycle
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

        // Refresh graph when skill count changes so skill nodes appear/disappear
        const prevActiveCount = (prevSkills ?? []).filter((s) => s.state === 'active').length;
        const nextActiveCount = regSkills.filter((s) => s.state === 'active').length;
        if (prevActiveCount !== nextActiveCount) {
          refreshNodesAndEdges();
        }
      } catch (err) {
        // 503 means the registry is not configured. That is the empty
        // teaching state, not a failure. Any other rejection leaves the
        // loaded list in place and records the message.
        if (!current()) return;
        if (isFeatureUnavailable(err)) {
          useRegistryStore.getState().setSkills([]);
          useRegistryStore.getState().setError(null);
        } else {
          useRegistryStore.getState().setError(failureMessage(err));
        }
      }

      // Fetch skill sources (provenance) independently. A failure here must not
      // affect the registry list above — the Library just falls back to
      // category grouping with no source headers/badges.
      try {
        const sources = await fetchSkillSources();
        if (!current()) return;
        useRegistryStore.getState().setSources(sources);
      } catch {
        // Sources unavailable — progressive disclosure, not an error.
      }
    } catch (error) {
      if (!current()) return;
      if (error instanceof AuthError) {
        setAuthRequired(true);
        setLoading(false);
        return;
      }

      // Differentiate network errors from HTTP errors. Pins and registry
      // fetches never ran, so record the same failure there or those
      // surfaces keep a spinner over stale "not loaded" state.
      const message =
        error instanceof GatewayRequestError && error.kind === 'connection'
          ? 'Gateway connection unavailable'
          : failureMessage(error);
      if (error instanceof GatewayRequestError && error.kind === 'connection') {
        // Network error: gateway unreachable (shutdown, crash, or network issue)
        setError(message);
        setConnectionStatus('disconnected');
      } else {
        // HTTP error: gateway is running but returned an error
        setError(message);
        setConnectionStatus('error');
      }
      if (!current()) return;
      usePinsStore.getState().setPinsError(message);
      usePinsStore.getState().setSkillPinsError(message);
      useRegistryStore.getState().setError(message);
    }
  }, [setGatewayStatus, setClients, setTools, setToolCatalog, setError, setConnectionStatus, setAuthRequired, setLoading, refreshNodesAndEdges, navigate]);

  useEffect(() => {
    if (!options?.registerRetry) return;
    registeredPoll = poll;
    return () => {
      if (registeredPoll === poll) registeredPoll = null;
    };
  }, [options?.registerRetry, poll]);

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
