import { useEffect, useState } from 'react';
import { Webhook } from 'lucide-react';
import { InspectorHeader } from '../../inspector';
import { Tab, Tabs } from '../../ui/Tabs';
import { Button } from '../../ui/Button';
import { StatePill } from '../../ui/StatePill';
import { showToast } from '../../ui/Toast';
import {
  approveHookPin,
  fetchHookPinDiff,
  fetchRegistryHook,
  HTTPError,
  syncHookProjections,
  unsyncHookProjections,
} from '../../../lib/api';
import { cn } from '../../../lib/cn';
import { describeError, isCancellation } from '../../../lib/describeError';
import { diffLines, MAX_DIFF_LINES, type DiffToken } from '../../../lib/diff';
import { escapeNonPrintable } from '../../../lib/nonPrintable';
import { describeHookSync, hookClientName, projectedScriptPath } from './hookModel';
import type { HookPinDiff, HookProjectionStatus, HookUnsupportedClient, RegistryHook } from '../../../types';

type HookTab = 'definition' | 'projection' | 'pin';

interface HookDetailPanelProps {
  hook: RegistryHook | null;
  statuses: HookProjectionStatus[];
  unsupported: HookUnsupportedClient[];
  onClose: () => void;
  onRefresh: () => Promise<void> | void;
}

export function HookDetailPanel({ hook, statuses, unsupported, onClose, onRefresh }: HookDetailPanelProps) {
  const [tab, setTab] = useState<HookTab>('definition');
  const [detail, setDetail] = useState<RegistryHook | null>(null);
  const name = hook?.name ?? null;

  useEffect(() => {
    if (!name) return;
    let cancelled = false;
    fetchRegistryHook(name)
      .then((full) => {
        if (!cancelled) setDetail(full);
      })
      .catch((err) => {
        if (!cancelled && !isCancellation(err)) {
          const described = describeError(err, 'Load hook');
          showToast('error', described.text, { detail: described.fullDetail });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [name]);

  if (!hook) {
    return (
      <div className="h-full flex items-center justify-center text-xs text-text-muted p-6">
        Select a hook to inspect it.
      </div>
    );
  }

  const shown = detail ?? hook;
  const scriptDir = statuses.find((row) => row.script_dir)?.script_dir;
  return (
    <div className="h-full flex flex-col min-h-0">
      <InspectorHeader title={hook.name} subtitle="Hook" icon={Webhook} accent="secondary" onClose={onClose} />
      <Tabs ariaLabel="Hook detail tabs">
        <Tab id="hook-tabbtn-definition" selected={tab === 'definition'} onSelect={() => setTab('definition')} controls="hook-tab-definition">Definition</Tab>
        <Tab id="hook-tabbtn-projection" selected={tab === 'projection'} onSelect={() => setTab('projection')} controls="hook-tab-projection">Projection</Tab>
        <Tab id="hook-tabbtn-pin" selected={tab === 'pin'} onSelect={() => setTab('pin')} controls="hook-tab-pin">Pin</Tab>
      </Tabs>
      <div className="flex-1 overflow-auto">
        {tab === 'definition' && (
          <div id="hook-tab-definition" role="tabpanel" aria-labelledby="hook-tabbtn-definition" className="p-4 flex flex-col gap-3">
            <p className="text-xs text-text-secondary">{shown.description}</p>
            <p className="text-2xs text-text-muted">
              A hook is edited by re-importing its pack. This inspector is read-only.
            </p>
            <p className="text-3xs uppercase tracking-wider text-text-muted">Events</p>
            <p className="text-xs font-mono">{(shown.events ?? []).join(', ') || 'none'}</p>
            <MatcherGroups hooks={shown.hooks} scriptDir={scriptDir} />
          </div>
        )}
        {tab === 'projection' && (
          <div id="hook-tab-projection" role="tabpanel" aria-labelledby="hook-tabbtn-projection" className="p-4 flex flex-col gap-3">
            <HookProjectionRows name={hook.name} statuses={statuses} onRefresh={onRefresh} />
            <UnsupportedChips clients={unsupported} />
          </div>
        )}
        {tab === 'pin' && (
          <div id="hook-tab-pin" role="tabpanel" aria-labelledby="hook-tabbtn-pin" className="p-4">
            <HookPinPanel name={hook.name} onRefresh={onRefresh} />
          </div>
        )}
      </div>
    </div>
  );
}

function MatcherGroups({ hooks, scriptDir }: { hooks: RegistryHook['hooks']; scriptDir?: string }) {
  const events = Object.keys(hooks ?? {});
  if (events.length === 0) {
    return <p className="text-2xs text-text-muted">Matcher groups load with the definition.</p>;
  }
  return (
    <div className="flex flex-col gap-2">
      {events.map((event) => (
        <div key={event} className="rounded-lg border border-border-subtle p-2">
          <p className="text-3xs uppercase tracking-wider text-text-muted mb-1">{event}</p>
          {(hooks?.[event] ?? []).map((group, i) => (
            <div key={i} className="mb-2">
              <p className="text-2xs text-text-secondary">matcher: {group.matcher || '(any)'}</p>
              {(group.hooks ?? []).map((handler, j) => (
                <p key={j} className="text-2xs font-mono text-text-primary break-all">
                  {handler.type}: {projectedScriptPath(handler.command, scriptDir)}
                  {handler.if ? ` if ${handler.if}` : ''}
                </p>
              ))}
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

function HookProjectionRows({
  name,
  statuses,
  onRefresh,
}: {
  name: string;
  statuses: HookProjectionStatus[];
  onRefresh: () => Promise<void> | void;
}) {
  const [busy, setBusy] = useState(false);
  const [drift, setDrift] = useState<HookProjectionStatus | null>(null);

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true);
    try {
      const result = await fn();
      if (Array.isArray(result)) {
        const described = describeHookSync(result);
        showToast(described.kind, described.message);
      }
      await onRefresh();
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Hook projection');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Button size="sm" disabled={busy} onClick={() => void act(() => syncHookProjections({ hooks: [name] }))}>
          Sync
        </Button>
        <Button size="sm" variant="secondary" disabled={busy} onClick={() => void act(() => unsyncHookProjections({ hooks: [name] }))}>
          Unsync
        </Button>
      </div>
      {statuses.length === 0 && <p className="text-2xs text-text-muted">Not projected yet.</p>}
      {statuses.map((row) => (
        <div key={row.client} className="rounded-lg border border-border-subtle p-2 flex flex-col gap-1">
          <div className="flex items-center gap-2">
            <StatePill state={row.state} />
            <span className="text-xs">{hookClientName(row.client)}</span>
          </div>
          <p className="text-2xs font-mono text-text-muted break-all">{row.target}</p>
          {row.script_dir && (
            <p className="text-2xs font-mono text-text-muted break-all">{row.script_dir}</p>
          )}
          {row.detail && <p className="text-2xs text-text-secondary">{row.detail}</p>}
          {row.state === 'drifted' && (
            <button type="button" className="text-2xs text-primary text-left" onClick={() => setDrift(row)}>
              Review drift
            </button>
          )}
        </div>
      ))}
      {drift && (
        <div role="dialog" aria-label="Drift review" className="rounded-lg border border-status-error/30 p-3 flex flex-col gap-2">
          <p className="text-xs">
            {hookClientName(drift.client)} was edited after gridctl wrote it. Force replaces the gridctl groups after a backup.
          </p>
          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={busy}
              onClick={() => void act(async () => {
                const result = await syncHookProjections({ hooks: [name], clients: [drift.client], force: true });
                setDrift(null);
                return result;
              })}
            >
              Force sync
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDrift(null)}>Close</Button>
          </div>
        </div>
      )}
    </div>
  );
}

function UnsupportedChips({ clients }: { clients: HookUnsupportedClient[] }) {
  if (clients.length === 0) return null;
  return (
    <div>
      <p className="text-3xs uppercase tracking-wider text-text-muted mb-1">Not a projection target</p>
      <div className="flex flex-wrap gap-1" aria-label="Unsupported clients">
        {clients.map((c) => (
          <span
            key={c.slug}
            title={c.location}
            className="text-3xs px-1.5 py-0.5 rounded border border-border/40 text-text-muted/80"
          >
            {c.name}{c.detected ? '' : ' (not detected)'}
          </span>
        ))}
      </div>
    </div>
  );
}

function HookPinPanel({ name, onRefresh }: { name: string; onRefresh: () => Promise<void> | void }) {
  const [diff, setDiff] = useState<HookPinDiff | null>(null);
  const [notPinned, setNotPinned] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    fetchHookPinDiff(name)
      .then((next) => {
        if (cancelled) return;
        setNotPinned(false);
        setLoadError('');
        setDiff(next);
      })
      .catch((err) => {
        if (cancelled || isCancellation(err)) return;
        setDiff(null);
        if (err instanceof HTTPError && err.status === 404) {
          setNotPinned(true);
          setLoadError('');
          return;
        }
        setNotPinned(false);
        setLoadError(describeError(err, 'Load hook pin diff').text);
      });
    return () => {
      cancelled = true;
    };
  }, [name]);

  const findings = diff?.findings ?? [];
  const findingsExist = findings.length > 0;
  const expectHash = diff?.composite_hash ?? '';
  const approveDisabled = busy || notPinned || loadError !== '' || expectHash === '' || (findingsExist && reason.trim() === '');
  const oldDocument = diff?.old_document ?? '';
  const newDocument = diff?.new_document ?? '';
  const documentChanged = diff !== null && oldDocument !== newDocument;

  const approve = async () => {
    if (approveDisabled) return;
    setBusy(true);
    try {
      await approveHookPin(name, expectHash, reason.trim());
      showToast('success', 'Hook pin approved');
      setReason('');
      await onRefresh();
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Approve hook pin');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setBusy(false);
    }
  };

  const statusLabel = notPinned ? 'not pinned' : diff?.status || (loadError ? 'unavailable' : 'loading');

  return (
    <div className="flex flex-col gap-2">
      <p className="text-xs">Pin status: <span className="font-mono">{statusLabel}</span></p>
      {notPinned && (
        <p className="text-xs text-text-muted">Not pinned; the next accepted import pins it</p>
      )}
      {loadError && <p className="text-xs text-status-error">{loadError}</p>}
      {findingsExist && (
        <ul aria-label="Pin findings" className="text-2xs text-status-pending">
          {findings.map((f, i) => (
            <li key={i}>{f.severity ? `${f.severity}: ` : ''}{f.message || 'finding'}</li>
          ))}
        </ul>
      )}
      {diff && (
        <>
          <HookFileChangeList label="Added" paths={diff.added_files} glyph="+" tone="text-status-running" />
          <HookFileChangeList label="Removed" paths={diff.removed_files} glyph="-" tone="text-status-error" />
          <HookFileChangeList label="Modified" paths={diff.modified_files} glyph="~" tone="text-status-pending" />
          {documentChanged ? (
            <HookDocumentDiff oldDocument={oldDocument} newDocument={newDocument} />
          ) : (
            <p className="text-2xs text-text-muted">hook.json unchanged</p>
          )}
        </>
      )}
      <label className="text-2xs text-text-muted" htmlFor="hook-pin-reason">
        Reason{findingsExist ? ' (required when findings exist)' : ''}
      </label>
      <textarea
        id="hook-pin-reason"
        aria-label="Approve reason"
        required={findingsExist}
        disabled={notPinned || loadError !== ''}
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        className="text-xs rounded border border-border bg-background p-2 min-h-16"
      />
      <Button size="sm" disabled={approveDisabled} onClick={() => void approve()}>
        Approve
      </Button>
    </div>
  );
}

function HookFileChangeList({
  label,
  paths,
  glyph,
  tone,
}: {
  label: string;
  paths: string[];
  glyph: string;
  tone: string;
}) {
  if (paths.length === 0) return null;
  return (
    <div className="text-2xs font-mono space-y-0.5" aria-label={`${label} files`}>
      {paths.map((p) => (
        <div key={p} className={cn('flex gap-1.5', tone)}>
          <span>{glyph}</span>
          <span className="break-all">{escapeNonPrintable(p)}</span>
        </div>
      ))}
    </div>
  );
}

function HookDocumentDiff({ oldDocument, newDocument }: { oldDocument: string; newDocument: string }) {
  const tokens = diffLines(oldDocument, newDocument);
  const changed = tokens.filter((t) => t.kind !== 'same').length;
  const shown = tokens.slice(0, MAX_DIFF_LINES);
  return (
    <div className="space-y-1">
      <h4 className="text-3xs uppercase tracking-[0.18em] text-text-muted/70">
        hook.json ({changed} {changed === 1 ? 'line' : 'lines'} changed)
      </h4>
      <div className="rounded border border-border/40 bg-background/60 overflow-x-auto max-h-40 overflow-y-auto" aria-label="Hook pin diff">
        <pre className="text-2xs font-mono leading-relaxed p-2 whitespace-pre-wrap break-words">
          {shown.map((token, i) => (
            <HookDiffLine key={i} token={token} />
          ))}
        </pre>
      </div>
      {tokens.length > MAX_DIFF_LINES && (
        <p className="text-3xs text-text-muted">Diff truncated at {MAX_DIFF_LINES} lines.</p>
      )}
    </div>
  );
}

function HookDiffLine({ token }: { token: DiffToken }) {
  const prefix = token.kind === 'added' ? '+ ' : token.kind === 'removed' ? '- ' : '  ';
  return (
    <span
      className={cn(
        'block px-1',
        token.kind === 'added' && 'bg-status-running/10 text-status-running',
        token.kind === 'removed' && 'bg-status-error/10 text-status-error',
        token.kind === 'same' && 'text-text-secondary',
      )}
    >
      {prefix}
      {escapeNonPrintable(token.text)}
    </span>
  );
}
