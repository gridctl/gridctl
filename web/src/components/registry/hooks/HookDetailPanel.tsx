import { useEffect, useState } from 'react';
import { Webhook } from 'lucide-react';
import { InspectorHeader } from '../../inspector';
import { Tab, Tabs } from '../../ui/Tabs';
import { Button } from '../../ui/Button';
import { StatePill } from '../../ui/StatePill';
import { ConfirmDialog } from '../../ui/ConfirmDialog';
import { showToast } from '../../ui/Toast';
import {
  fetchRegistryHook,
  syncHookProjections,
  unsyncHookProjections,
} from '../../../lib/api';
import { describeError, isCancellation } from '../../../lib/describeError';
import { escapeNonPrintable } from '../../../lib/nonPrintable';
import {
  describeHookSync,
  describeHookUnsync,
  hookClientName,
  joinProse,
  planHookUnsync,
  projectedScriptPath,
} from './hookModel';
import { HookPinReview } from './HookPinReview';
import type {
  HookProjectionStatus,
  HookSyncResult,
  HookUnsupportedClient,
  HookUnsyncResult,
  RegistryHook,
} from '../../../types';

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
            <HookProjectionRows name={hook.name} statuses={statuses} unsupported={unsupported} onRefresh={onRefresh} />
            <UnsupportedChips clients={unsupported} />
          </div>
        )}
        {tab === 'pin' && (
          <div id="hook-tab-pin" role="tabpanel" aria-labelledby="hook-tabbtn-pin" className="p-4">
            <HookPinReview name={hook.name} hookDir={detail?.dir} onRefresh={onRefresh} />
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

type Described = { kind: 'success' | 'error'; message: string };

function HookProjectionRows({
  name,
  statuses,
  unsupported,
  onRefresh,
}: {
  name: string;
  statuses: HookProjectionStatus[];
  unsupported: HookUnsupportedClient[];
  onRefresh: () => Promise<void> | void;
}) {
  const [busy, setBusy] = useState(false);
  const [drift, setDrift] = useState<HookProjectionStatus | null>(null);
  const [confirmUnsync, setConfirmUnsync] = useState(false);

  // onSettled runs once the request settles, before the toast and refresh.
  const act = async <T,>(fn: () => Promise<T>, describe: (result: T) => Described, onSettled?: () => void) => {
    setBusy(true);
    try {
      let result: T;
      try {
        result = await fn();
      } finally {
        onSettled?.();
      }
      const described = describe(result);
      showToast(described.kind, described.message);
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

  const plan = planHookUnsync(statuses, unsupported);
  const unsyncBlocked =
    statuses.length === 0
      ? 'Not projected to any client'
      : plan.removable.length === 0
        ? 'Every projection has drifted. Review the drift first.'
        : '';

  const unsync = () =>
    act<HookUnsyncResult[]>(
      () => unsyncHookProjections({ hooks: [name] }),
      describeHookUnsync,
      () => setConfirmUnsync(false),
    );

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          disabled={busy}
          onClick={() => void act<HookSyncResult[]>(() => syncHookProjections({ hooks: [name] }), describeHookSync)}
        >
          Sync
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={busy || unsyncBlocked !== ''}
          title={unsyncBlocked || undefined}
          onClick={() => setConfirmUnsync(true)}
        >
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
          <p className="text-2xs text-text-muted" data-testid="drift-no-diff">
            gridctl cannot show this diff in the UI yet. Compare the file at{' '}
            <span className="font-mono break-all">{escapeNonPrintable(drift.target)}</span>
            {drift.script_dir ? (
              <>
                {' '}and the scripts in{' '}
                <span className="font-mono break-all">{escapeNonPrintable(drift.script_dir)}</span>
              </>
            ) : null}{' '}
            with the hook's canonical definition before choosing.
          </p>
          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={busy}
              onClick={() => void act<HookSyncResult[]>(async () => {
                const result = await syncHookProjections({ hooks: [name], clients: [drift.client], force: true });
                setDrift(null);
                return result;
              }, describeHookSync)}
            >
              Force sync
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setDrift(null)}>Close</Button>
          </div>
        </div>
      )}
      <ConfirmDialog
        isOpen={confirmUnsync}
        onClose={() => setConfirmUnsync(false)}
        onConfirm={() => void unsync()}
        title={`Unsync ${name}?`}
        message={<UnsyncMessage plan={plan} />}
        confirmLabel={`Unsync from ${plan.removable.length} client${plan.removable.length === 1 ? '' : 's'}`}
        busy={busy}
      />
    </div>
  );
}

function UnsyncMessage({ plan }: { plan: ReturnType<typeof planHookUnsync> }) {
  const removable = joinProse(plan.removable.map(hookClientName));
  const kept = joinProse(plan.kept.map(hookClientName));
  return (
    <>
      <p>
        Removes this hook's entries and script copies from {removable}.{' '}
        {plan.removable.length === 1 ? 'That client stops' : 'Those clients stop'} running it.
      </p>
      {plan.covered.map(({ names, by }) => (
        <p key={by}>
          {joinProse(names)} {names.length === 1 ? 'reads' : 'read'} the {hookClientName(by)} projection and{' '}
          {names.length === 1 ? 'stops' : 'stop'} running it too.
        </p>
      ))}
      {plan.kept.length > 0 && (
        <p>
          {kept} {plan.kept.length === 1 ? 'was' : 'were'} edited after gridctl wrote{' '}
          {plan.kept.length === 1 ? 'it. Its projection is' : 'them. Their projections are'} kept until the drift is
          reviewed.
        </p>
      )}
      <p>The registry copy and its pin stay. Sync restores the projection.</p>
    </>
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
            className="text-3xs px-1.5 py-0.5 rounded border border-border/40 text-text-muted"
          >
            {c.name}
            {c.covered_by ? ` via ${c.covered_by}` : ''}
            {c.detected ? '' : ' (not detected)'}
          </span>
        ))}
      </div>
    </div>
  );
}
