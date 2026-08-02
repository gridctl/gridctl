import { useCallback, useEffect, useMemo } from 'react';
import { useSearchParams } from 'react-router';
import { Loader2, Pin } from 'lucide-react';
import { cn } from '../../lib/cn';
import {
  serverHasAlertFindings,
  countServerAlertFindings,
  skillHasAlertFindings,
  countSkillAlertFindings,
  usePinsStore,
} from '../../stores/usePinsStore';
import {
  fetchServerPins,
  fetchSkillPins,
  resetServerPins,
  resetSkillPin,
  type ServerPins,
  type SkillPin,
} from '../../lib/api';
import { useListNav } from '../../hooks/useListNav';
import { useUIStore } from '../../stores/useUIStore';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { PinsRail, type PinsKind } from '../pins/PinsRail';
import { PinsServerDetail, TOOLS_SECTION_ID } from '../pins/PinsServerDetail';
import { PinsSkillDetail } from '../pins/PinsSkillDetail';
import { APPROVE_BUTTON_ID, DRIFT_SECTION_ID } from '../pins/PinsDriftSection';
import { usePinsCommands } from '../pins/usePinsCommands';
import { showToast } from '../ui/Toast';

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
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.pins);
  const pinsPrefs = useUIStore((s) => s.pinsPrefs);
  const pins = usePinsStore((s) => s.pins);
  const skillPins = usePinsStore((s) => s.skillPins);

  // Only the value 'skill' selects the skill kind; anything else (including
  // a hand-edited URL) is the server kind, matching the pre-kind URLs.
  const kind: PinsKind = searchParams.get('kind') === 'skill' ? 'skill' : 'server';

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
  const selectedParam = kind === 'server' ? serverParam : skillParam;
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
  const attentionDefault = useMemo(
    () =>
      kind === 'server'
        ? entries.some(needsAttention)
        : skillEntries.some(skillNeedsAttention),
    [kind, entries, needsAttention, skillEntries, skillNeedsAttention],
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

  const activeVisible = kind === 'server' ? visibleEntries : visibleSkillEntries;

  const activeName = useMemo(() => {
    if (activeVisible.some(([name]) => name === selectedParam)) return selectedParam;
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

  const selectionParamName = kind === 'server' ? 'server' : 'skill';

  const applySelection = useCallback(
    (name: string) => {
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
        if (next === 'skill') {
          p.set('kind', 'skill');
        } else {
          p.delete('kind');
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
      try {
        await resetServerPins(name);
        const updated = await fetchServerPins();
        usePinsStore.getState().setPins(updated);
        showToast('success', `Pins reset for ${name}; it re-pins on the next verify`);
        updateParams((p) => {
          if (p.get('server') === name) p.delete('server');
        });
      } catch (err) {
        showToast('error', `Failed to reset: ${err instanceof Error ? err.message : 'Unknown error'}`);
      }
    },
    [updateParams],
  );

  const handleResetSkill = useCallback(
    async (name: string) => {
      try {
        await resetSkillPin(name);
        const updated = await fetchSkillPins();
        usePinsStore.getState().setSkillPins(updated);
        showToast('success', `Pin reset for ${name}; it re-pins on the next registry refresh`);
        updateParams((p) => {
          if (p.get('skill') === name) {
            p.delete('skill');
            p.delete('view');
          }
        });
      } catch (err) {
        showToast('error', `Failed to reset: ${err instanceof Error ? err.message : 'Unknown error'}`);
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
  useEffect(() => {
    if (!view || !activeName) return;
    // A drifted skill's findings render inside the drift section; scrolling
    // to the digests table would sail straight past them.
    const id =
      view === 'drift' || (kind === 'skill' && view === 'findings' && activeSkillDrifted)
        ? DRIFT_SECTION_ID
        : TOOLS_SECTION_ID;
    const frame = requestAnimationFrame(() => {
      document.getElementById(id)?.scrollIntoView?.({ block: 'nearest' });
    });
    return () => cancelAnimationFrame(frame);
  }, [view, activeName, kind, activeSkillDrifted]);

  const selectedIndex = activeVisible.findIndex(([name]) => name === activeName);
  useListNav({
    itemCount: activeVisible.length,
    selectedIndex: selectedIndex < 0 ? 0 : selectedIndex,
    setSelectedIndex: (i) => {
      const name = activeVisible[i]?.[0];
      if (name) applySelection(name);
    },
    onEnter: () => {
      // useListNav preventDefaults Enter at the document level, which also
      // cancels native button activation - so the second Enter must click
      // programmatically or a keyboard-only user could focus Approve but
      // never press it.
      const approve = document.getElementById(APPROVE_BUTTON_ID);
      if (!approve) return;
      if (document.activeElement === approve) {
        (approve as HTMLButtonElement).click();
      } else {
        approve.focus();
      }
    },
  });

  usePinsCommands({
    activeServerName: activeName,
    toggleAttention,
    toggleFindingsOnly,
  });

  // null means the first poll for the active kind has not landed yet; both
  // endpoints return an empty object (not an error) when pinning is quiet.
  const activeMap = kind === 'server' ? pins : skillPins;
  const activeCount = kind === 'server' ? entries.length : skillEntries.length;

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
            totalCount={activeCount}
            activeName={activeName}
            onSelect={applySelection}
            attentionOnly={attentionOnly}
            onToggleAttention={toggleAttention}
            isOutsideFilter={(name) =>
              attentionOnly &&
              (kind === 'server'
                ? !entries.filter(needsAttention).some(([n]) => n === name)
                : !skillEntries.filter(skillNeedsAttention).some(([n]) => n === name))
            }
          />
        }
        minLeftPx={220}
      >
        <main className="flex flex-col h-full overflow-hidden">
          <header
            className={cn(
              'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 flex items-center gap-3',
              compact ? 'py-2' : 'py-3',
            )}
          >
            <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">
              pins
            </div>
            <div className="font-mono text-[10px] text-text-muted">
              {kind === 'server' ? headerTally(entries) : skillHeaderTally(skillEntries)}
            </div>
          </header>

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
            {activeMap === null ? (
              <PinsEmptyState
                icon={<Loader2 size={24} className="text-primary/70 animate-spin" />}
                title="Loading pins…"
                body="Fetching pin state from the gateway."
              />
            ) : activeCount === 0 ? (
              kind === 'server' ? (
                <PinsEmptyState
                  icon={<Pin size={24} className="text-primary/70" />}
                  title="No servers pinned yet"
                  body="Servers are pinned automatically on first verify after deploy. If schema pinning is disabled in your stack, nothing will appear here."
                />
              ) : (
                <PinsEmptyState
                  icon={<Pin size={24} className="text-primary/70" />}
                  title="No skills pinned yet"
                  body="Registry skills are pinned automatically when the daemon first observes them. Add a skill to the Library and it appears here after the next refresh."
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
              </>
            )}
          </div>
        </main>
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

// ---------------------------------------------------------------------------
// Empty states
// ---------------------------------------------------------------------------

function PinsEmptyState({
  icon,
  title,
  body,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="absolute inset-0 flex items-center justify-center bg-background px-6 py-12">
      <div className="max-w-md w-full text-center space-y-4">
        <div className="mx-auto w-14 h-14 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center">
          {icon}
        </div>
        <div className="space-y-1.5">
          <h2 className="text-base font-semibold text-text-primary">{title}</h2>
          <p className="text-xs text-text-muted leading-relaxed">{body}</p>
        </div>
      </div>
    </div>
  );
}

export default PinsWorkspace;
