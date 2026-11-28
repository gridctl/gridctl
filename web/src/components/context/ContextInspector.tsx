import { useCallback, useEffect, useId, useRef, useState } from 'react';
import { Link } from 'react-router';
import { ChevronDown, ChevronRight, Globe } from 'lucide-react';
import { cn } from '../../lib/cn';
import { InspectorHeader, InspectorTabButton, InspectorTabList } from '../inspector';
import { TabPanel } from '../ui/Tabs';
import { StatePill } from '../ui/StatePill';
import { PackChip } from '../registry/PackChip';
import { showToast } from '../ui/Toast';
import { useGuardedLinkClick } from '../../hooks/useGuardedNavigate';
import { useContextStore } from '../../stores/useContextStore';
import { contextPinsUrl, type ContextPinChipStatus } from '../../lib/contextGovernance';
import { describeError, isCancellation } from '../../lib/describeError';
import {
  syncGlobalContext,
  unsyncGlobalContext,
  type ContextClientStatus,
  type ContextFragment,
  type ContextFragmentStatus,
  type ContextState,
} from '../../lib/api';

// States where the user must act on a client's projection.
const ATTENTION_STATES: ContextState[] = ['drifted', 'stale', 'target-missing'];

export type ContextInspectorTab = 'clients' | 'details';

export type ContextInspectorDetails =
  | {
      mode: 'single-file';
      canonicalPath: string;
      lineCount: number;
      charCount: number;
      pinStatus: ContextPinChipStatus;
    }
  | {
      mode: 'fragments';
      fragment: ContextFragment | null;
      fragmentPath: string | null;
      outOfSyncClients: string[];
      pinStatus: ContextPinChipStatus | null;
    };

export interface ContextInspectorProps {
  clients: ContextClientStatus[];
  tab: ContextInspectorTab;
  onTabChange: (tab: ContextInspectorTab) => void;
  onReviewDrift: (slug: string, fragment?: string) => void;
  spotlightSlug?: string | null;
  details: ContextInspectorDetails;
}

function summarizeClients(clients: ContextClientStatus[]): string {
  const counts = clients.reduce<Record<string, number>>((acc, c) => {
    acc[c.state] = (acc[c.state] ?? 0) + 1;
    return acc;
  }, {});
  return (['in-sync', 'stale', 'drifted', 'target-missing', 'never-synced', 'unsupported'] as const)
    .filter((s) => counts[s])
    .map((s) => `${counts[s]} ${s}`)
    .join(' · ');
}

function pinStatusLabel(status: ContextPinChipStatus): string {
  if (status === 'drift') return 'Pin drift';
  if (status === 'pinned') return 'Pinned';
  return 'Unpinned';
}

/**
 * Right-rail inspector for the Library's Context segment. Clients lists every
 * client's projection state and actions; Details surfaces read-only metadata
 * and pin governance links for the current file or selected fragment.
 */
export function ContextInspector({
  clients,
  tab,
  onTabChange,
  onReviewDrift,
  spotlightSlug = null,
  details,
}: ContextInspectorProps) {
  const idBase = useId();
  const clientsTabId = `${idBase}-clients-tab`;
  const clientsPanelId = `${idBase}-clients-panel`;
  const detailsTabId = `${idBase}-details-tab`;
  const detailsPanelId = `${idBase}-details-panel`;

  const attentionCount = clients.filter((c) => ATTENTION_STATES.includes(c.state)).length;
  const subtitleText =
    attentionCount > 0
      ? `${attentionCount} ${attentionCount === 1 ? 'client needs' : 'clients need'} attention`
      : summarizeClients(clients) || 'No clients';

  return (
    <aside
      aria-label="Global context inspector"
      className="h-full flex flex-col bg-surface/50 border-l border-border/50 overflow-hidden"
    >
      <InspectorHeader
        title="Global Context"
        icon={Globe}
        accent="primary"
        subtitle={
          <span
            className={cn(
              'text-2xs truncate',
              attentionCount > 0 ? 'text-status-pending' : 'text-text-muted',
            )}
          >
            {subtitleText}
          </span>
        }
      />

      <InspectorTabList ariaLabel="Global context inspector sections">
        <InspectorTabButton
          id={clientsTabId}
          active={tab === 'clients'}
          onClick={() => onTabChange('clients')}
          label="Clients"
          controls={clientsPanelId}
        />
        <InspectorTabButton
          id={detailsTabId}
          active={tab === 'details'}
          onClick={() => onTabChange('details')}
          label="Details"
          controls={detailsPanelId}
        />
      </InspectorTabList>

      <TabPanel
        id={clientsPanelId}
        labelledBy={clientsTabId}
        active={tab === 'clients'}
        className="flex-1 min-h-0 overflow-y-auto scrollbar-dark"
      >
        {clients.length === 0 ? (
          <p className="text-xs text-text-muted p-4">No clients reported.</p>
        ) : (
          <ul className="divide-y divide-border/20">
            {clients.map((c) => (
              <ClientRow
                key={c.slug}
                client={c}
                onReviewDrift={onReviewDrift}
                spotlight={c.slug === spotlightSlug}
              />
            ))}
          </ul>
        )}
      </TabPanel>

      <TabPanel
        id={detailsPanelId}
        labelledBy={detailsTabId}
        active={tab === 'details'}
        className="flex-1 min-h-0 overflow-y-auto scrollbar-dark p-4"
      >
        {tab === 'details' && <DetailsPanel details={details} />}
      </TabPanel>
    </aside>
  );
}

function DetailsPanel({ details }: { details: ContextInspectorDetails }) {
  if (details.mode === 'single-file') {
    return (
      <dl className="flex flex-col gap-3 text-xs">
        <DetailItem label="Canonical file">
          <span className="font-mono text-2xs text-text-primary break-all">{details.canonicalPath}</span>
        </DetailItem>
        <DetailItem label="Size">
          <span className="font-mono text-2xs text-text-secondary">
            {details.lineCount} {details.lineCount === 1 ? 'line' : 'lines'} · {details.charCount}{' '}
            {details.charCount === 1 ? 'char' : 'chars'}
          </span>
        </DetailItem>
        <DetailItem label="Pin status">
          <PinStatusLine name="AGENTS.md" status={details.pinStatus} />
        </DetailItem>
      </dl>
    );
  }

  if (!details.fragment) {
    return <p className="text-xs text-text-muted">Select a fragment to see its details.</p>;
  }

  const f = details.fragment;
  const paths = f.paths ?? [];

  return (
    <dl className="flex flex-col gap-3 text-xs">
      <DetailItem label="Fragment">
        <span className="font-mono text-text-primary break-all">{f.name}</span>
      </DetailItem>
      {details.fragmentPath && (
        <DetailItem label="File path">
          <span className="font-mono text-2xs text-text-secondary break-all">{details.fragmentPath}</span>
        </DetailItem>
      )}
      <div className="grid grid-cols-2 gap-3">
        <DetailItem label="Position">
          <span className="font-mono text-2xs text-text-secondary">{f.position}</span>
        </DetailItem>
        <DetailItem label="Size">
          <span className="font-mono text-2xs text-text-secondary">{f.bytes} bytes</span>
        </DetailItem>
      </div>
      {f.description && (
        <DetailItem label="Description">
          <span className="text-text-secondary">{f.description}</span>
        </DetailItem>
      )}
      {paths.length > 0 && (
        <DetailItem label="Paths">
          <ul className="flex flex-col gap-1">
            {paths.map((glob) => (
              <li key={glob} className="font-mono text-2xs text-text-secondary break-all">
                {glob}
              </li>
            ))}
          </ul>
        </DetailItem>
      )}
      <DetailItem label="Out-of-sync clients">
        {details.outOfSyncClients.length > 0 ? (
          <span className="text-status-pending">{details.outOfSyncClients.join(', ')}</span>
        ) : (
          <span className="text-text-muted">None</span>
        )}
      </DetailItem>
      {details.pinStatus && (
        <DetailItem label="Pin status">
          <PinStatusLine name={f.name} status={details.pinStatus} />
        </DetailItem>
      )}
    </dl>
  );
}

function DetailItem({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1 min-w-0">
      <dt className="text-3xs uppercase tracking-wider text-text-muted">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

function PinStatusLine({ name, status }: { name: string; status: ContextPinChipStatus }) {
  const guardedLinkClick = useGuardedLinkClick();
  const href = contextPinsUrl(name, status === 'drift' ? 'drift' : undefined);
  return (
    <div className="flex items-center gap-2 flex-wrap">
      <span
        className={cn(
          'text-xs font-medium',
          status === 'drift'
            ? 'text-status-pending'
            : status === 'pinned'
              ? 'text-status-running'
              : 'text-text-muted',
        )}
      >
        {pinStatusLabel(status)}
      </span>
      <Link
        to={href}
        onClick={(e) => guardedLinkClick(e, href)}
        className="text-2xs text-primary hover:text-primary-light underline underline-offset-2"
      >
        Review in Pins
      </Link>
    </div>
  );
}

/**
 * One client's row with inline actions, stacked on two lines for the narrow
 * right rail. In fragments mode a multi-file row with non-synced fragments
 * grows a chevron that expands one line per fragment (StatePill, name, pack
 * provenance, and Review on drifted lines).
 */
function ClientRow({
  client: c,
  onReviewDrift,
  spotlight = false,
}: {
  client: ContextClientStatus;
  onReviewDrift: (slug: string, fragment?: string) => void;
  spotlight?: boolean;
}) {
  const refresh = useContextStore((s) => s.refresh);
  const [busy, setBusy] = useState(false);
  const fragments = c.fragments ?? [];
  const [expanded, setExpanded] = useState(spotlight && fragments.length > 0);
  // A spotlighted row whose fragments arrive after mount (the deep link
  // landed on a stale doc) still expands when the refresh delivers them.
  const [hadFragments, setHadFragments] = useState(fragments.length > 0);
  if ((fragments.length > 0) !== hadFragments) {
    setHadFragments(fragments.length > 0);
    if (spotlight && fragments.length > 0) setExpanded(true);
  }
  const rowRef = useRef<HTMLLIElement>(null);
  useEffect(() => {
    if (spotlight) rowRef.current?.scrollIntoView?.({ block: 'nearest' });
  }, [spotlight]);

  const act = useCallback(
    async (fn: () => Promise<unknown>, okMessage: string) => {
      setBusy(true);
      try {
        await fn();
        showToast('success', okMessage);
        await refresh();
      } catch (err) {
        if (!isCancellation(err)) {
          const described = describeError(err, 'Action');
          showToast('error', described.text, { detail: described.fullDetail });
        }
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  return (
    <li
      ref={rowRef}
      data-spotlight={spotlight ? 'true' : undefined}
      className={cn('px-4 py-2.5', spotlight && 'bg-primary/5')}
    >
      <div className="flex items-center gap-1.5 min-w-0 flex-wrap">
        {fragments.length > 0 && (
          <button
            type="button"
            onClick={() => setExpanded((e) => !e)}
            aria-expanded={expanded}
            aria-label={`${expanded ? 'Collapse' : 'Expand'} ${c.name} fragments`}
            className="p-0.5 -ml-1 rounded text-text-muted hover:text-text-primary transition-colors flex-shrink-0"
          >
            {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
          </button>
        )}
        <StatePill state={c.state} />
        {c.mode && (
          <span
            title="How this client receives the context in fragments mode"
            className="text-3xs px-1.5 py-0.5 rounded border border-border/40 bg-background/40 text-text-muted font-mono whitespace-nowrap"
          >
            {c.mode}
          </span>
        )}
        <span className="text-xs font-medium text-text-primary truncate min-w-0">
          {c.name}
          {c.unofficial && c.supported && (
            <span
              className="ml-1 text-3xs font-normal text-text-secondary"
              title="Path sourced unofficially; it may move without an upstream release note"
            >
              (unofficial path)
            </span>
          )}
        </span>
      </div>
      <div className="mt-1 flex items-center gap-2 min-w-0">
        <span
          className="text-2xs text-text-muted font-mono truncate flex-1 min-w-0"
          title={c.target_path ?? c.detail}
        >
          {c.supported ? c.target_path : c.detail}
          {c.supported && !c.available && c.state === 'never-synced' && ' (client not detected)'}
        </span>
        <span className="flex items-center gap-1.5 flex-shrink-0">
          {c.state === 'drifted' && (
            <ClientAction label="Review" onClick={() => onReviewDrift(c.slug)} disabled={busy} />
          )}
          {(c.state === 'stale' ||
            c.state === 'target-missing' ||
            (c.state === 'never-synced' && c.available)) && (
            <ClientAction
              label={busy ? 'Syncing…' : 'Sync'}
              disabled={busy}
              onClick={() =>
                void act(() => syncGlobalContext({ clients: [c.slug] }), `${c.name} synced`)
              }
            />
          )}
          {(c.state === 'in-sync' || c.state === 'stale' || c.state === 'drifted') && (
            <ClientAction
              label="Unsync"
              subtle
              disabled={busy}
              onClick={() => void act(() => unsyncGlobalContext(c.slug), `${c.name} unsynced`)}
            />
          )}
        </span>
      </div>
      {expanded && fragments.length > 0 && (
        <ul className="mt-1.5 ml-4 flex flex-col gap-1" aria-label={`${c.name} fragments`}>
          {fragments.map((f) => (
            <FragmentStatusLine
              key={`${c.slug}:${f.name}`}
              clientSlug={c.slug}
              fragment={f}
              busy={busy}
              onReviewDrift={onReviewDrift}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

/** One non-synced fragment line under an expanded multi-file client row. */
function FragmentStatusLine({
  clientSlug,
  fragment: f,
  busy,
  onReviewDrift,
}: {
  clientSlug: string;
  fragment: ContextFragmentStatus;
  busy: boolean;
  onReviewDrift: (slug: string, fragment?: string) => void;
}) {
  return (
    <li className="flex items-center gap-2 min-w-0">
      <StatePill state={f.state} />
      <span className="text-2xs text-text-secondary font-mono truncate flex-1 min-w-0">{f.name}</span>
      {f.pack && <PackChip pack={f.pack} />}
      {f.state === 'drifted' && (
        <ClientAction
          label="Review"
          disabled={busy}
          onClick={() => onReviewDrift(clientSlug, f.name)}
        />
      )}
    </li>
  );
}

function ClientAction({
  label,
  onClick,
  disabled,
  subtle,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  subtle?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'px-2 py-0.5 rounded-md text-2xs font-medium border transition-colors disabled:opacity-50',
        subtle
          ? 'text-text-muted border-border/40 hover:bg-surface-highlight'
          : 'text-primary border-primary/25 hover:bg-primary/10',
      )}
    >
      {label}
    </button>
  );
}
