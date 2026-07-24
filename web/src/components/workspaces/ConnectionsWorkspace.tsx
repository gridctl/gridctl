import { createElement, useCallback, useEffect, useMemo, useState } from 'react';
import { Loader2, Plug } from 'lucide-react';
import { cn } from '../../lib/cn';
import {
  ClientLinkError,
  fetchClients,
  linkClient,
  previewClientLink,
  unlinkClient,
  type ClientLinkPreview,
} from '../../lib/api';
import { getClientIcon } from '../../lib/clientIcons';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { useStackStore } from '../../stores/useStackStore';
import type { ClientStatus } from '../../types';
import { Modal } from '../ui/Modal';
import { showToast } from '../ui/Toast';

// Desired connection state per slug, staged locally until Apply. Only
// slugs whose desired state differs from the current state are present.
type StagedChanges = Record<string, boolean>;

// The toggle reflects "connected in any sense": linked (an entry exists in
// the client config, however it got there) or declared in the stack's
// link: block. Toggling ON an already-linked client adopts it into link:;
// the Declared badge still distinguishes declared from merely linked.
function isConnected(c: ClientStatus): boolean {
  return c.linked || Boolean(c.declared);
}

/**
 * Connections workspace: link LLM clients to the gateway from the UI, kept
 * in lockstep with the stack.yaml link: block (each apply writes both).
 * Deliberately labeled Connections, not Clients — per-client access scoping
 * (the clients: block) lives in the Tools workspace and is a different
 * concern.
 *
 * Toggles stage changes locally; Apply opens a review dialog with a
 * per-client config diff (nothing is written until confirmed).
 */
export default function ConnectionsWorkspace() {
  const clients = useStackStore((s) => s.clients);
  const [staged, setStaged] = useState<StagedChanges>({});
  const [reviewing, setReviewing] = useState(false);
  const [applying, setApplying] = useState(false);

  const changes = useMemo(
    () =>
      clients
        .filter((c) => c.slug in staged && staged[c.slug] !== isConnected(c))
        .map((c) => ({ client: c, enable: staged[c.slug] })),
    [clients, staged],
  );

  const toggle = useCallback((c: ClientStatus) => {
    setStaged((prev) => {
      const current = isConnected(c);
      const desired = !(c.slug in prev ? prev[c.slug] : current);
      const next = { ...prev };
      if (desired === current) {
        delete next[c.slug];
      } else {
        next[c.slug] = desired;
      }
      return next;
    });
  }, []);

  const refresh = useCallback(async () => {
    try {
      useStackStore.getState().setClients(await fetchClients());
    } catch {
      // Polling refreshes shortly anyway.
    }
  }, []);

  const apply = useCallback(async () => {
    setApplying(true);
    const failed = new Set<string>();
    for (const { client, enable } of changes) {
      try {
        if (enable) {
          await linkClient(client.slug);
        } else {
          await unlinkClient(client.slug);
        }
      } catch (err) {
        failed.add(client.slug);
        const detail =
          err instanceof ClientLinkError && err.hint
            ? `${err.message} ${err.hint}`
            : err instanceof Error
              ? err.message
              : String(err);
        showToast('error', `${client.name}: ${detail}`);
      }
    }
    if (failed.size === 0) {
      showToast('success', `Applied ${changes.length} connection change${changes.length === 1 ? '' : 's'}`);
    }
    // Failed changes stay staged so they remain visible for retry; applied
    // ones clear (the refresh below picks up their new server state).
    setStaged((prev) =>
      Object.fromEntries(Object.entries(prev).filter(([slug]) => failed.has(slug))),
    );
    setReviewing(false);
    setApplying(false);
    await refresh();
  }, [changes, refresh]);

  if (clients.length === 0) {
    return (
      <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
        <ConnectionsHeader subtitle="No clients reported" />
        <EmptyState
          title="No client registry available"
          body="Start a stack with 'gridctl apply' to detect and link LLM clients."
        />
      </div>
    );
  }

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <ConnectionsHeader
        subtitle={`${clients.filter((c) => c.linked).length} linked · ${clients.filter((c) => c.detected).length} detected · access scoping lives in Tools`}
      />

      <div className="flex-1 overflow-y-auto px-6 py-4">
        <div className="max-w-3xl mx-auto flex flex-col gap-2">
          {clients.map((c) => (
            <ClientRow
              key={c.slug}
              client={c}
              desired={c.slug in staged ? staged[c.slug] : isConnected(c)}
              onToggle={() => toggle(c)}
            />
          ))}
        </div>
      </div>

      {changes.length > 0 && (
        <div className="flex-shrink-0 border-t border-border-subtle bg-surface px-6 py-3">
          <div className="max-w-3xl mx-auto flex items-center justify-between">
            <span className="text-xs text-text-secondary">
              {changes.length} pending change{changes.length === 1 ? '' : 's'}
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={() => setStaged({})}
                className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:bg-surface-highlight/50"
              >
                Discard
              </button>
              <button
                onClick={() => setReviewing(true)}
                className="px-3 py-1.5 text-xs rounded-lg bg-primary/10 text-primary border border-primary/30 hover:bg-primary/20"
              >
                Review &amp; Apply
              </button>
            </div>
          </div>
        </div>
      )}

      {reviewing && (
        <ReviewDialog
          changes={changes}
          applying={applying}
          onApply={apply}
          onClose={() => setReviewing(false)}
        />
      )}
    </div>
  );
}

function ConnectionsHeader({ subtitle }: { subtitle: string }) {
  return (
    <div className="flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 py-3">
      <div className="flex items-baseline gap-3">
        <h1 className="text-xs font-medium uppercase tracking-[0.4em] text-text-primary">
          Connections
        </h1>
        <span className="font-mono text-[10px] text-text-muted">{subtitle}</span>
      </div>
    </div>
  );
}

function ClientRow({
  client,
  desired,
  onToggle,
}: {
  client: ClientStatus;
  desired: boolean;
  onToggle: () => void;
}) {
  const canLink = client.detected;
  const staged = desired !== isConnected(client);

  return (
    <div
      className={cn(
        'flex items-center gap-4 rounded-xl border px-4 py-3 bg-surface',
        staged ? 'border-primary/40' : 'border-border-subtle',
      )}
    >
      <div className="w-9 h-9 rounded-lg bg-surface-elevated border border-border-subtle flex items-center justify-center text-text-secondary">
        <ClientBrandIcon slug={client.slug} size={18} />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-text-primary">{client.name}</span>
          <Badges client={client} />
        </div>
        <div className="font-mono text-[10px] text-text-muted truncate">
          {client.detected ? (client.configPath ?? client.transport) : 'Not installed on this machine'}
        </div>
      </div>
      <button
        role="switch"
        aria-checked={desired}
        aria-label={`Link ${client.name}`}
        disabled={!canLink && !desired && !staged}
        onClick={onToggle}
        title={!canLink && !desired ? 'Client not detected on this machine' : undefined}
        className={cn(
          'relative w-9 h-5 rounded-full transition-colors flex-shrink-0 border',
          // On: solid accent track. Off: grayed track with a visible border
          // so the two states cannot be confused in either theme.
          desired
            ? 'bg-primary border-primary'
            : 'bg-text-muted/20 border-text-muted/30 opacity-70',
          !canLink && !desired && !staged && 'opacity-40 cursor-not-allowed',
        )}
      >
        <span
          className={cn(
            'absolute top-0.5 left-0 w-4 h-4 rounded-full bg-white shadow-sm transition-transform',
            desired ? 'translate-x-[18px]' : 'translate-x-0.5',
          )}
        />
      </button>
    </div>
  );
}

// ClientBrandIcon resolves the brand mark per render via createElement so no
// component value is created during render (react-hooks/static-components).
function ClientBrandIcon({ slug, size }: { slug: string; size?: number }) {
  return createElement(getClientIcon(slug), { size });
}

function Badges({ client }: { client: ClientStatus }) {
  return (
    <span className="flex items-center gap-1">
      {client.linked && (
        <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-status-running/15 text-status-running">
          Linked
        </span>
      )}
      {client.declared && (
        <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-primary/15 text-primary">
          Declared
        </span>
      )}
      {client.detected && !client.linked && (
        <span className="text-[9px] uppercase tracking-wide px-1.5 py-0.5 rounded-full bg-surface-elevated text-text-muted">
          Detected
        </span>
      )}
    </span>
  );
}

function ReviewDialog({
  changes,
  applying,
  onApply,
  onClose,
}: {
  changes: { client: ClientStatus; enable: boolean }[];
  applying: boolean;
  onApply: () => void;
  onClose: () => void;
}) {
  return (
    <Modal isOpen onClose={onClose} title="Review connection changes" size="wide">
      <div className="flex flex-col gap-4 max-h-[60vh] overflow-y-auto pr-1">
        {changes.map(({ client, enable }) =>
          enable ? (
            <LinkPreviewCard key={client.slug} client={client} />
          ) : (
            <div key={client.slug} className="rounded-lg border border-border-subtle bg-surface p-3">
              <div className="text-sm text-text-primary mb-1">Unlink {client.name}</div>
              <div className="text-xs text-text-muted">
                Removes the gateway entry from{' '}
                <span className="font-mono">{client.configPath ?? 'its config'}</span> and the link:
                declaration from stack.yaml.
              </div>
            </div>
          ),
        )}
      </div>
      <div className="flex items-center justify-end gap-2 pt-4">
        <button
          onClick={onClose}
          className="px-3 py-1.5 text-xs rounded-lg text-text-secondary hover:bg-surface-highlight/50"
        >
          Cancel
        </button>
        <button
          onClick={onApply}
          disabled={applying}
          className="px-3 py-1.5 text-xs rounded-lg bg-primary/10 text-primary border border-primary/30 hover:bg-primary/20 disabled:opacity-50 flex items-center gap-1.5"
        >
          {applying && <Loader2 size={12} className="animate-spin" />}
          Apply changes
        </button>
      </div>
    </Modal>
  );
}

// LinkPreviewCard fetches the dry-run diff for one pending link on mount.
// Failures degrade to a text note; the Apply itself will surface real
// errors.
function LinkPreviewCard({ client }: { client: ClientStatus }) {
  const [preview, setPreview] = useState<ClientLinkPreview | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    previewClientLink(client.slug)
      .then((p) => {
        if (!cancelled) setPreview(p);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [client.slug]);

  return (
    <div className="rounded-lg border border-border-subtle bg-surface p-3">
      <div className="flex items-center gap-2 mb-1">
        <Plug size={12} className="text-primary" />
        <span className="text-sm text-text-primary">Link {client.name}</span>
        {preview && (
          <span className="font-mono text-[10px] text-text-muted truncate">{preview.configPath}</span>
        )}
      </div>
      {error && <div className="text-xs text-status-error">Preview unavailable: {error}</div>}
      {!preview && !error && (
        <div className="text-xs text-text-muted flex items-center gap-1.5">
          <Loader2 size={11} className="animate-spin" /> Computing diff…
        </div>
      )}
      {preview && (
        <div className="grid grid-cols-2 gap-2 mt-2">
          <DiffPane label="Current" text={preview.before} />
          <DiffPane label="After" text={preview.after} />
        </div>
      )}
      {preview?.stackDiff && (
        <details className="mt-2">
          <summary className="text-[10px] uppercase tracking-wide text-text-muted cursor-pointer">
            stack.yaml change
          </summary>
          <pre className="mt-1 text-[10px] font-mono text-text-secondary bg-surface-elevated rounded-md p-2 overflow-x-auto whitespace-pre-wrap break-words max-h-40 overflow-y-auto">
            {escapeNonPrintable(preview.stackDiff)}
          </pre>
        </details>
      )}
    </div>
  );
}

function DiffPane({ label, text }: { label: string; text: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[10px] uppercase tracking-wide text-text-muted mb-1">{label}</div>
      <pre className="text-[10px] font-mono text-text-secondary bg-surface-elevated rounded-md p-2 overflow-x-auto whitespace-pre-wrap break-words max-h-40 overflow-y-auto">
        {escapeNonPrintable(text) || '(empty)'}
      </pre>
    </div>
  );
}

function EmptyState({ title, body }: { title: string; body: string }) {
  return (
    <div className="flex-1 flex items-center justify-center">
      <div className="text-center max-w-xs">
        <div className="w-14 h-14 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center mx-auto mb-4 text-primary">
          <Plug size={22} />
        </div>
        <div className="text-sm font-medium text-text-primary mb-1">{title}</div>
        <div className="text-xs text-text-muted">{body}</div>
      </div>
    </div>
  );
}
