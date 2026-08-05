import { useEffect, useState } from 'react';
import { Loader2, Plug } from 'lucide-react';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { previewClientLink, type ClientLinkPreview } from '../../lib/api';
import type { ClientStatus } from '../../types';
import { Modal } from '../ui/Modal';

/**
 * Review & Apply dialog for staged connection changes, unchanged from the
 * single-column page: it batches changes across clients, so it stays a
 * workspace-level surface rather than folding into one client's detail
 * pane (which would hide changes to unselected clients from the reviewer).
 */
export function ReviewDialog({
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
