import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import { Loader2, Pin } from 'lucide-react';
import { requestGatewayPoll } from '../../hooks/usePolling';
import { EmptyState, FailedState, FailureBanner } from '../ui/EmptyState';
import { WORKSPACE_LABELS } from '../../types/workspace';
import {
  serverHasAlertFindings,
  countServerAlertFindings,
  skillHasAlertFindings,
  countSkillAlertFindings,
  contextHasAlertFindings,
  hookHasAlertFindings,
  countHookAlertFindings,
  usePinsStore,
} from '../../stores/usePinsStore';
import {
  fetchServerPins,
  fetchSkillPins,
  fetchContextPins,
  resetServerPins,
  resetSkillPin,
  resetContextPin,
  type ServerPins,
  type SkillPin,
  type ContextPin,
} from '../../lib/api';
import type { HookPin } from '../../types';
import { useListNav } from '../../hooks/useListNav';
import { useUIStore } from '../../stores/useUIStore';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { PinsRail, type PinsKind } from '../pins/PinsRail';
import { PinsServerDetail, TOOLS_SECTION_ID } from '../pins/PinsServerDetail';
import { PinsSkillDetail } from '../pins/PinsSkillDetail';
import { PinsContextDetail } from '../pins/PinsContextDetail';
import { PinsHookDetail } from '../pins/PinsHookDetail';
import { DRIFT_SECTION_ID } from '../pins/PinsDriftSection';
import { usePinsCommands } from '../pins/usePinsCommands';
import { showToast } from '../ui/Toast';
import { WorkspaceHeader } from '../ui/WorkspaceHeader';
import { describeError, isCancellation } from '../../lib/describeError';
import { escapeNonPrintable } from '../../lib/nonPrintable';

// Valid ?view= targets: drift scrolls to the drift panel, findings and tools
// both land on the pinned-records table (findings live inside it).
const PINS_VIEWS = ['drift', 'findings', 'tools'] as const;
type PinsView = (typeof PINS_VIEWS)[number];

// PinsWorkspace is the pinning surface, sibling to Stack, Library, Variables,
// Tools, and Metrics, covering both pin kinds behind one review grammar:
// server tool-schema pins and skill document pins. The left rail carries the
// kind toggle and lists pinned items (drifted first, filtered to items
// needing attention by default when any exist); the center pane shows the
// selected item's drift diff (when any) with the Approve action beside it,
// followed by its pinned records. State is URL-first (?kind=, ?server=,
// ?skill=, ?view=, ?attention=), with the persisted attention preference
// seeding bare visits.
export function PinsWorkspace() {
  const headingId = useId();
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.pins);
  const pinsPrefs = useUIStore((s) => s.pinsPrefs);
  const pins = usePinsStore((s) => s.pins);
  const skillPins = usePinsStore((s) => s.skillPins);
  const pinsError = usePinsStore((s) => s.pinsError);
  const skillPinsError = usePinsStore((s) => s.skillPinsError);
  const contextPins = usePinsStore((s) => s.contextPins);
  const contextPinsError = usePinsStore((s) => s.contextPinsError);
  const hookPins = usePinsStore((s) => s.hookPins);
  const hookPinsError = usePinsStore((s) => s.hookPinsError);

  // skill and context are explicit. Anything else, including a hand-edited
  // URL, stays the server kind so pre-kind URLs keep landing on servers.
  const kindParam = searchParams.get('kind');
  const kind: PinsKind =
    kindParam === 'skill' || kindParam === 'context' || kindParam === 'hook' ? kindParam : 'server';

  // Drifted servers first, then alphabetical, for a stable rail order that
  // surfaces what needs attention.
  const entries = useMemo(() => {
    if (!pins) return [];
    return Object.entries(pins).sort(([aName, a], [bName, b]) => {
      const aDrift = a.status === 'drift' ? 0 : 1;
      const bDrift = b.status === 'drift' ? 0 : 1;
      if (aDrift !== bDrift) return aDrift - bDrift;
      return aName.localeCompare(bName);
    });
  }, [pins]);

  // Skills: drifted first; drifted entries sort by origin repo so shared-
  // origin drift is consecutive (the rail groups those runs under a source
  // header), then alphabetical within and after.
  // Sorts ungrouped (origin-less) drifted skills after every repo group.
  const SORT_LAST = '\uffff';
  const skillEntries = useMemo(() => {
    if (!skillPins) return [];
    return Object.entries(skillPins).sort(([aName, a], [bName, b]) => {
      const aDrift = a.status === 'drift' ? 0 : 1;
      const bDrift = b.status === 'drift' ? 0 : 1;
      if (aDrift !== bDrift) return aDrift - bDrift;
      if (aDrift === 0) {
        const aRepo = a.origin?.repo ?? SORT_LAST;
        const bRepo = b.origin?.repo ?? SORT_LAST;
        if (aRepo !== bRepo) return aRepo.localeCompare(bRepo);
      }
      return aName.localeCompare(bName);
    });
  }, [skillPins]);

  const contextEntries = useMemo(() => {
    if (!contextPins) return [];
    return Object.entries(contextPins).sort(([aName, a], [bName, b]) => {
      const aDrift = a.status === 'drift' ? 0 : 1;
      const bDrift = b.status === 'drift' ? 0 : 1;
      if (aDrift !== bDrift) return aDrift - bDrift;
      return aName.localeCompare(bName);
    });
  }, [contextPins]);

  const hookEntries = useMemo(() => {
    if (!hookPins) return [];
    return Object.entries(hookPins).sort(([aName, a], [bName, b]) => {
      const aDrift = a.status === 'drift' ? 0 : 1;
      const bDrift = b.status === 'drift' ? 0 : 1;
      if (aDrift !== bDrift) return aDrift - bDrift;
      return aName.localeCompare(bName);
    });
  }, [hookPins]);

  const updateParams = useCallback(
    (mutate: (p: URLSearchParams) => void) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          mutate(next);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const serverParam = searchParams.get('server') ?? '';
  const skillParam = searchParams.get('skill') ?? '';
  const fragmentParam = searchParams.get('fragment') ?? '';
  const hookParam = searchParams.get('hook') ?? '';
  const selectedParam =
    kind === 'server' ? serverParam : kind === 'skill' ? skillParam : kind === 'context' ? fragmentParam : hookParam;
  const viewParam = searchParams.get('view');
  const view: PinsView | null = PINS_VIEWS.includes(viewParam as PinsView)
    ? (viewParam as PinsView)
    : null;

  // Attention filter: URL param wins, then the user's persisted choice, then
  // the computed default (on only when something needs attention).
  const needsAttention = useCallback(
    ([, sp]: [string, ServerPins]) => sp.status === 'drift' || serverHasAlertFindings(sp),
    [],
  );
  const skillNeedsAttention = useCallback(
    ([, pin]: [string, SkillPin]) => pin.status === 'drift' || skillHasAlertFindings(pin),
    [],
  );
  const contextNeedsAttention = useCallback(
    ([, pin]: [string, ContextPin]) => pin.status === 'drift' || contextHasAlertFindings(pin),
    [],
  );
  const hookNeedsAttention = useCallback(
    ([, pin]: [string, HookPin]) => pin.status === 'drift' || hookHasAlertFindings(pin),
    [],
  );
  const attentionDefault = useMemo(
    () =>
      kind === 'server'
        ? entries.some(needsAttention)
        : kind === 'skill'
          ? skillEntries.some(skillNeedsAttention)
          : kind === 'context'
            ? contextEntries.some(contextNeedsAttention)
            : hookEntries.some(hookNeedsAttention),
    [
      kind,
      entries,
      needsAttention,
      skillEntries,
      skillNeedsAttention,
      contextEntries,
      contextNeedsAttention,
      hookEntries,
      hookNeedsAttention,
    ],
  );
  // Only the two self-produced values are honored; anything else (a
  // hand-edited URL) falls through to the pref/default instead of silently
  // meaning "off".
  const attentionParam = searchParams.get('attention');
  const attentionOnly =
    attentionParam === '1'
      ? true
      : attentionParam === '0'
        ? false
        : (pinsPrefs.attentionOnly ?? attentionDefault);

  // The active selection stays visible even when the filter would drop it,
  // so a deep link never lands on a silently reselected item.
  const visibleEntries = useMemo(() => {
    if (!attentionOnly) return entries;
    return entries.filter((e) => needsAttention(e) || e[0] === serverParam);
  }, [entries, attentionOnly, needsAttention, serverParam]);

  const visibleSkillEntries = useMemo(() => {
    if (!attentionOnly) return skillEntries;
    return skillEntries.filter((e) => skillNeedsAttention(e) || e[0] === skillParam);
  }, [skillEntries, attentionOnly, skillNeedsAttention, skillParam]);

  const visibleContextEntries = useMemo(() => {
    if (!attentionOnly) return contextEntries;
    return contextEntries.filter((e) => contextNeedsAttention(e) || e[0] === fragmentParam);
  }, [contextEntries, attentionOnly, contextNeedsAttention, fragmentParam]);

  const visibleHookEntries = useMemo(() => {
    if (!attentionOnly) return hookEntries;
    return hookEntries.filter((e) => hookNeedsAttention(e) || e[0] === hookParam);
  }, [hookEntries, attentionOnly, hookNeedsAttention, hookParam]);

  const activeVisible =
    kind === 'server'
      ? visibleEntries
      : kind === 'skill'
        ? visibleSkillEntries
        : kind === 'context'
          ? visibleContextEntries
          : visibleHookEntries;

  // A named selection that is not pinned resolves to nothing rather than the
  // first row, so a stale link never mounts another item's Approve under it.
  const activeName = useMemo(() => {
    if (activeVisible.some(([name]) => name === selectedParam)) return selectedParam;
    if (selectedParam) return '';
    return activeVisible[0]?.[0] ?? '';
  }, [activeVisible, selectedParam]);

  const activePins = useMemo(
    () =>
      kind === 'server'
        ? (visibleEntries.find(([name]) => name === activeName)?.[1] ?? null)
        : null,
    [kind, visibleEntries, activeName],
  );
  const activeSkillPin = useMemo(
    () =>
      kind === 'skill'
        ? (visibleSkillEntries.find(([name]) => name === activeName)?.[1] ?? null)
        : null,
    [kind, visibleSkillEntries, activeName],
  );
  const activeContextPin = useMemo(
    () =>
      kind === 'context'
        ? (visibleContextEntries.find(([name]) => name === activeName)?.[1] ?? null)
        : null,
    [kind, visibleContextEntries, activeName],
  );
  const activeHookPin = useMemo(
    () =>
      kind === 'hook'
        ? (visibleHookEntries.find(([name]) => name === activeName)?.[1] ?? null)
        : null,
    [kind, visibleHookEntries, activeName],
  );

  const selectionParamName =
    kind === 'server' ? 'server' : kind === 'skill' ? 'skill' : kind === 'context' ? 'fragment' : 'hook';

  // Counts explicit pin choices. activeName also moves on a kind switch or a
  // filter change, which must not close the navigation drawer.
  const [pinChoice, setPinChoice] = useState(0);

  const applySelection = useCallback(
    (name: string) => {
      setPinChoice((n) => n + 1);
      updateParams((p) => {
        p.set(selectionParamName, name);
        // ?view= is a one-shot landing intent from a deep link; an explicit
        // selection is a new intent, so the old target must not keep
        // hijacking the scroll position on every rail move.
        p.delete('view');
      });
    },
    [updateParams, selectionParamName],
  );

  const applyKind = useCallback(
    (next: PinsKind) => {
      updateParams((p) => {
        if (next === 'server') {
          p.delete('kind');
        } else {
          p.set('kind', next);
        }
        // A kind switch is a new intent; the old view target belongs to the
        // other kind's pane. Selections persist per kind in their own params.
        p.delete('view');
      });
    },
    [updateParams],
  );

  // Make an automatic selection explicit: without the selection param in the
  // URL, the fallback row could silently change when a poll clears its drift
  // (or vanish entirely under the attention filter after an approve).
  // Writing it once pins the review target; view= is preserved so a deep
  // link's scroll intent still lands. Guarded per kind so switching kinds
  // never fights the other kind's param.
  useEffect(() => {
    if (!selectedParam && activeName) {
      updateParams((p) => {
        p.set(selectionParamName, activeName);
      });
    }
  }, [selectedParam, activeName, updateParams, selectionParamName]);

  // null means the first poll for the active kind has not landed yet; both
  // endpoints return an empty object (not an error) when pinning is quiet.
  const activeMap =
    kind === 'server' ? pins : kind === 'skill' ? skillPins : kind === 'context' ? contextPins : hookPins;

  // The name a reset in this workspace is removing. The reset handlers update
  // the store before deleting the param, possibly in separate renders, so the
  // stale-selection effect skips the warning for this name. Cleared when the
  // selection moves off it or the reset fails, so it cannot silence a later
  // real stale link.
  const resettingRef = useRef<string | null>(null);

  // A selection naming an item that is not pinned (a stale deep link, or an
  // item removed elsewhere) is announced and dropped from the URL; the effect
  // above then writes the first row on the next render.
  useEffect(() => {
    if (resettingRef.current !== null && resettingRef.current !== selectedParam) {
      resettingRef.current = null;
    }
    if (activeMap === null || !selectedParam) return;
    if (Object.prototype.hasOwnProperty.call(activeMap, selectedParam)) return;
    if (resettingRef.current === selectedParam) {
      resettingRef.current = null;
    } else {
      showToast('warning', `${escapeNonPrintable(selectedParam)} is no longer pinned`);
    }
    updateParams((p) => {
      p.delete(selectionParamName);
      p.delete('view');
    });
  }, [activeMap, selectedParam, selectionParamName, updateParams]);

  // Findings-only table filter: ?view=findings is the landing intent, the
  // persisted pref carries the explicit choice between visits.
  const findingsOnly = view === 'findings' || pinsPrefs.findingsOnly;
  const toggleFindingsOnly = useCallback(() => {
    const next = !(useUIStore.getState().pinsPrefs.findingsOnly || view === 'findings');
    useUIStore.getState().setPinsPrefs({ findingsOnly: next });
    if (!next && view === 'findings') {
      updateParams((p) => {
        p.delete('view');
      });
    }
  }, [view, updateParams]);

  const handleReset = useCallback(
    async (name: string) => {
      resettingRef.current = name;
      try {
        await resetServerPins(name);
        const updated = await fetchServerPins();
        usePinsStore.getState().setPins(updated);
        showToast('success', `Pins reset for ${name}; it re-pins on the next verify`);
        updateParams((p) => {
          if (p.get('server') === name) p.delete('server');
        });
      } catch (err) {
        resettingRef.current = null;
        if (!isCancellation(err)) {
          const described = describeError(err, `Reset pins for ${name}`);
          showToast('error', described.text, { detail: described.fullDetail });
        }
      }
    },
    [updateParams],
  );

  const handleResetContext = useCallback(
    async (name: string) => {
      resettingRef.current = name;
      try {
        await resetContextPin(name);
        const updated = await fetchContextPins();
        usePinsStore.getState().setContextPins(updated);
        updateParams((p) => {
          if (p.get('fragment') === name) {
            p.delete('fragment');
            p.delete('view');
          }
        });
      } catch (err) {
        resettingRef.current = null;
        throw err;
      }
    },
    [updateParams],
  );

  const handleResetSkill = useCallback(
    async (name: string) => {
      resettingRef.current = name;
      try {
        await resetSkillPin(name);
        const updated = await fetchSkillPins();
        usePinsStore.getState().setSkillPins(updated);
        showToast('success', `Pin reset for ${name}; it re-pins on the next Library refresh`);
        updateParams((p) => {
          if (p.get('skill') === name) {
            p.delete('skill');
            p.delete('view');
          }
        });
      } catch (err) {
        resettingRef.current = null;
        if (!isCancellation(err)) {
          const described = describeError(err, `Reset pins for ${name}`);
          showToast('error', described.text, { detail: described.fullDetail });
        }
      }
    },
    [updateParams],
  );

  const toggleAttention = useCallback(() => {
    const next = !attentionOnly;
    // Persist the explicit choice; the URL carries it only when it differs
    // from the computed default so canonical views keep clean URLs.
    useUIStore.getState().setPinsPrefs({ attentionOnly: next });
    updateParams((p) => {
      if (next === attentionDefault) {
        p.delete('attention');
      } else {
        p.set('attention', next ? '1' : '0');
      }
    });
  }, [attentionOnly, attentionDefault, updateParams]);

  // ?view= targeting: scroll the requested section into view once the
  // selection has rendered. rAF defers past the commit; scrollIntoView is
  // absent in jsdom, hence the optional call.
  const activeSkillDrifted = activeSkillPin?.status === 'drift';
  const activeContextDrifted = activeContextPin?.status === 'drift';
  useEffect(() => {
    if (!view || !activeName) return;
    // A drifted skill's or fragment's findings render inside the drift
    // section; scrolling to the pinned content would sail straight past them.
    // The hook review renders findings inside that section whether or not
    // the hook is drifted.
    const id =
      view === 'drift' ||
      (view === 'findings' && kind === 'hook') ||
      (view === 'findings' &&
        ((kind === 'skill' && activeSkillDrifted) || (kind === 'context' && activeContextDrifted)))
        ? DRIFT_SECTION_ID
        : TOOLS_SECTION_ID;
    const frame = requestAnimationFrame(() => {
      document.getElementById(id)?.scrollIntoView?.({ block: 'nearest' });
    });
    return () => cancelAnimationFrame(frame);
  }, [view, activeName, kind, activeSkillDrifted, activeContextDrifted]);

  const selectedIndex = activeVisible.findIndex(([name]) => name === activeName);
  useListNav({
    itemCount: activeVisible.length,
    selectedIndex: selectedIndex < 0 ? 0 : selectedIndex,
    setSelectedIndex: (i) => {
      const name = activeVisible[i]?.[0];
      if (name) applySelection(name);
    },
  });

  usePinsCommands({
    activeServerName: activeName,
    toggleAttention,
    toggleFindingsOnly,
  });

  const activeError =
    kind === 'server' ? pinsError : kind === 'skill' ? skillPinsError : kind === 'context' ? contextPinsError : hookPinsError;
  const activeCount =
    kind === 'server'
      ? entries.length
      : kind === 'skill'
        ? skillEntries.length
        : kind === 'context'
          ? contextEntries.length
          : hookEntries.length;

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="pins"
        defaultLeftPct={20}
        left={
          <PinsRail
            compact={compact}
            kind={kind}
            onKindChange={applyKind}
            entries={visibleEntries}
            skillEntries={visibleSkillEntries}
            contextEntries={visibleContextEntries}
            hookEntries={visibleHookEntries}
            totalCount={activeCount}
            activeName={activeName}
            onSelect={applySelection}
            attentionOnly={attentionOnly}
            onToggleAttention={toggleAttention}
            isOutsideFilter={(name) =>
              attentionOnly &&
              (kind === 'server'
                ? !entries.filter(needsAttention).some(([n]) => n === name)
                : kind === 'skill'
                  ? !skillEntries.filter(skillNeedsAttention).some(([n]) => n === name)
                  : kind === 'context'
                    ? !contextEntries.filter(contextNeedsAttention).some(([n]) => n === name)
                    : !hookEntries.filter(hookNeedsAttention).some(([n]) => n === name))
            }
          />
        }
        minLeftPx={220}
        leftKey={String(pinChoice)}
      >
        <section aria-labelledby={headingId} className="flex flex-col h-full overflow-hidden">
          <WorkspaceHeader
            title={WORKSPACE_LABELS.pins}
            headingId={headingId}
            compact={compact}
            summary={(
              <span className="font-mono">
                {kind === 'server'
                  ? headerTally(entries)
                  : kind === 'skill'
                    ? skillHeaderTally(skillEntries)
                    : kind === 'context'
                      ? `${contextEntries.length} ${contextEntries.length === 1 ? 'fragment' : 'fragments'} pinned`
                      : hookHeaderTally(hookEntries)}
              </span>
            )}
          />

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark flex flex-col">
            {activeError && activeMap === null ? (
              <FailedState
                feature="Pins"
                message={activeError}
                onRetry={requestGatewayPoll}
                className="flex-1"
              />
            ) : activeMap === null ? (
              <EmptyState
                icon={Loader2}
                iconClassName="animate-spin"
                tone="primary"
                title="Loading pins…"
                description="Fetching pin state from the gateway."
                className="flex-1"
              />
            ) : (
              <>
                {activeError && (
                  <FailureBanner message={activeError} onRetry={requestGatewayPoll} />
                )}
                {activeCount === 0 ? (
                  kind === 'server' ? (
                    <EmptyState
                      icon={Pin}
                      tone="primary"
                      title="No servers pinned yet"
                      description="Servers are pinned automatically on first verify after deploy. If schema pinning is disabled in your stack, nothing will appear here."
                      className="flex-1"
                    />
                  ) : kind === 'skill' ? (
                    <EmptyState
                      icon={Pin}
                      tone="primary"
                      title="No skills pinned yet"
                      description="Library skills are pinned automatically when the daemon first observes them. Add a skill to the Library and it appears here after the next refresh."
                      className="flex-1"
                    />
                  ) : kind === 'hook' ? (
                    <EmptyState
                      icon={Pin}
                      tone="primary"
                      title="No hooks pinned yet"
                      description="Hooks are pinned when a pack that carries them is imported. Import a pack with hooks and they appear here after the next refresh."
                      className="flex-1"
                    />
                  ) : (
                    <EmptyState
                      icon={Pin}
                      tone="primary"
                      title="No fragment pins yet"
                      description="Context fragments are pinned on the next sync. Run gridctl ctx sync, then review drift here."
                      className="flex-1"
                    />
                  )
                ) : (
                  <>
                    {kind === 'server' && activePins && (
                      <PinsServerDetail
                        key={activeName}
                        name={activeName}
                        pins={activePins}
                        findingsOnly={findingsOnly}
                        onToggleFindingsOnly={toggleFindingsOnly}
                        onReset={handleReset}
                        expandFindingsOnMount={view === 'findings'}
                      />
                    )}
                    {kind === 'skill' && activeSkillPin && (
                      <PinsSkillDetail
                        key={activeName}
                        name={activeName}
                        pin={activeSkillPin}
                        onReset={handleResetSkill}
                      />
                    )}
                    {kind === 'context' && activeContextPin && (
                      <PinsContextDetail
                        key={activeName}
                        name={activeName}
                        pin={activeContextPin}
                        onReset={handleResetContext}
                      />
                    )}
                    {kind === 'hook' && activeHookPin && (
                      <PinsHookDetail key={activeName} name={activeName} pin={activeHookPin} />
                    )}
                  </>
                )}
              </>
            )}
          </div>
        </section>
      </WorkspaceShell>
    </div>
  );
}

// headerTally summarizes the rail: "N servers pinned · X drifted · Y with
// findings", omitting zero segments so a quiet stack reads as before. Always
// computed over ALL pins, never the attention-filtered view.
function headerTally(entries: Array<[string, ServerPins]>): string {
  const drifted = entries.filter(([, sp]) => sp.status === 'drift').length;
  const withFindings = entries.filter(([, sp]) => countServerAlertFindings(sp) > 0).length;
  const parts = [`${entries.length} ${entries.length === 1 ? 'server' : 'servers'} pinned`];
  if (drifted > 0) parts.push(`${drifted} drifted`);
  if (withFindings > 0) parts.push(`${withFindings} with findings`);
  return parts.join(' · ');
}

function skillHeaderTally(entries: Array<[string, SkillPin]>): string {
  const drifted = entries.filter(([, pin]) => pin.status === 'drift').length;
  const withFindings = entries.filter(([, pin]) => countSkillAlertFindings(pin) > 0).length;
  const parts = [`${entries.length} ${entries.length === 1 ? 'skill' : 'skills'} pinned`];
  if (drifted > 0) parts.push(`${drifted} drifted`);
  if (withFindings > 0) parts.push(`${withFindings} with findings`);
  return parts.join(' · ');
}

function hookHeaderTally(entries: Array<[string, HookPin]>): string {
  const drifted = entries.filter(([, pin]) => pin.status === 'drift').length;
  const withFindings = entries.filter(([, pin]) => countHookAlertFindings(pin) > 0).length;
  const parts = [`${entries.length} ${entries.length === 1 ? 'hook' : 'hooks'} pinned`];
  if (drifted > 0) parts.push(`${drifted} drifted`);
  if (withFindings > 0) parts.push(`${withFindings} with findings`);
  return parts.join(' · ');
}

export default PinsWorkspace;
