import { useCallback, useEffect, useMemo } from 'react';
import { useSearchParams } from 'react-router';
import { Loader2, Pin } from 'lucide-react';
import { cn } from '../../lib/cn';
import { serverHasAlertFindings, countServerAlertFindings, usePinsStore } from '../../stores/usePinsStore';
import { fetchServerPins, resetServerPins, type ServerPins } from '../../lib/api';
import { useListNav } from '../../hooks/useListNav';
import { useUIStore } from '../../stores/useUIStore';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { PinsRail } from '../pins/PinsRail';
import { PinsServerDetail, TOOLS_SECTION_ID } from '../pins/PinsServerDetail';
import { APPROVE_BUTTON_ID, DRIFT_SECTION_ID } from '../pins/PinsDriftSection';
import { usePinsCommands } from '../pins/usePinsCommands';
import { showToast } from '../ui/Toast';

// Valid ?view= targets: drift scrolls to the drift panel, findings and tools
// both land on the pinned-records table (findings live inside it).
const PINS_VIEWS = ['drift', 'findings', 'tools'] as const;
type PinsView = (typeof PINS_VIEWS)[number];

// PinsWorkspace is the schema-pinning surface, sibling to Stack, Library,
// Variables, Tools, and Metrics. The left rail lists pinned servers (drifted
// first, filtered to servers needing attention by default when any exist);
// the center pane shows the selected server's drift diff (when any) with the
// Approve action beside it, followed by its pinned tool records. State is
// URL-first (?server=, ?view=, ?attention=), with the persisted attention
// preference seeding bare visits.
export function PinsWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.pins);
  const pinsPrefs = useUIStore((s) => s.pinsPrefs);
  const pins = usePinsStore((s) => s.pins);

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
  const attentionDefault = useMemo(() => entries.some(needsAttention), [entries, needsAttention]);
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

  // The active ?server= stays visible even when the filter would drop it, so
  // a deep link never lands on a silently reselected server.
  const visibleEntries = useMemo(() => {
    if (!attentionOnly) return entries;
    return entries.filter((e) => needsAttention(e) || e[0] === serverParam);
  }, [entries, attentionOnly, needsAttention, serverParam]);

  const activeServerName = useMemo(() => {
    if (visibleEntries.some(([name]) => name === serverParam)) return serverParam;
    return visibleEntries[0]?.[0] ?? '';
  }, [visibleEntries, serverParam]);

  const activePins = useMemo(
    () => visibleEntries.find(([name]) => name === activeServerName)?.[1] ?? null,
    [visibleEntries, activeServerName],
  );

  const applyServer = useCallback(
    (name: string) => {
      updateParams((p) => {
        p.set('server', name);
        // ?view= is a one-shot landing intent from a deep link; an explicit
        // selection is a new intent, so the old target must not keep
        // hijacking the scroll position on every rail move.
        p.delete('view');
      });
    },
    [updateParams],
  );

  // Make an automatic selection explicit: without ?server= in the URL, the
  // fallback row could silently change when a poll clears its drift (or
  // vanish entirely under the attention filter after an approve). Writing it
  // once pins the review target; view= is preserved so a deep link's scroll
  // intent still lands.
  useEffect(() => {
    if (!serverParam && activeServerName) {
      updateParams((p) => {
        p.set('server', activeServerName);
      });
    }
  }, [serverParam, activeServerName, updateParams]);

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
  useEffect(() => {
    if (!view || !activeServerName) return;
    const id = view === 'drift' ? DRIFT_SECTION_ID : TOOLS_SECTION_ID;
    const frame = requestAnimationFrame(() => {
      document.getElementById(id)?.scrollIntoView?.({ block: 'nearest' });
    });
    return () => cancelAnimationFrame(frame);
  }, [view, activeServerName]);

  const selectedIndex = visibleEntries.findIndex(([name]) => name === activeServerName);
  useListNav({
    itemCount: visibleEntries.length,
    selectedIndex: selectedIndex < 0 ? 0 : selectedIndex,
    setSelectedIndex: (i) => {
      const name = visibleEntries[i]?.[0];
      if (name) applyServer(name);
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
    activeServerName,
    toggleAttention,
    toggleFindingsOnly,
  });

  // null means the first /api/pins poll has not landed yet; the endpoint
  // returns an empty object (not an error) when pinning is disabled.
  if (pins === null) {
    return (
      <PinsEmptyState
        icon={<Loader2 size={24} className="text-primary/70 animate-spin" />}
        title="Loading pins…"
        body="Fetching pin state from the gateway."
      />
    );
  }

  if (entries.length === 0) {
    return (
      <PinsEmptyState
        icon={<Pin size={24} className="text-primary/70" />}
        title="No servers pinned yet"
        body="Servers are pinned automatically on first verify after deploy. If schema pinning is disabled in your stack, nothing will appear here."
      />
    );
  }

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="pins"
        defaultLeftPct={20}
        left={
          <PinsRail
            compact={compact}
            entries={visibleEntries}
            totalCount={entries.length}
            activeServerName={activeServerName}
            onSelect={applyServer}
            attentionOnly={attentionOnly}
            onToggleAttention={toggleAttention}
            isOutsideFilter={(name) =>
              attentionOnly &&
              !entries.filter(needsAttention).some(([n]) => n === name)
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
            <div className="font-mono text-[10px] text-text-muted">{headerTally(entries)}</div>
          </header>

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
            {activePins && (
              <PinsServerDetail
                key={activeServerName}
                name={activeServerName}
                pins={activePins}
                findingsOnly={findingsOnly}
                onToggleFindingsOnly={toggleFindingsOnly}
                onReset={handleReset}
                expandFindingsOnMount={view === 'findings'}
              />
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
