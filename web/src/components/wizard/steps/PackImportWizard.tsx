import { useCallback, useId, useState } from 'react';
import { useNavigate } from 'react-router';
import { AlertTriangle, ArrowLeft, CheckCircle2, Download, Loader2, Package } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { Button } from '../../ui/Button';
import { showToast } from '../../ui/Toast';
import { useWizardStore } from '../../../stores/useWizardStore';
import {
  addPack,
  applyPack,
  PackFindingsError,
  previewPack,
  type PackAddDoc,
  type PackPreview,
  type PackPreviewResource,
} from '../../../lib/api';
import { describeApplyDoc } from '../../registry/packs/packModel';

type PackStep = 'source' | 'review' | 'done';

/**
 * The Pack import flow: repo URL, a read-only resolved-selection review
 * (packs select by manifest, so there are no per-item checkboxes), one
 * pack-wide trust acknowledgment when any resource carries security
 * findings (mirroring the CLI's single --trust), then install with a
 * success step that emphasizes Apply.
 */
export function PackImportWizard() {
  const navigate = useNavigate();
  const close = useWizardStore((s) => s.close);
  const trustId = useId();

  const [step, setStep] = useState<PackStep>('source');
  const [repo, setRepo] = useState('');
  const [ref, setRef] = useState('');
  const [path, setPath] = useState('');
  const [loading, setLoading] = useState(false);
  const [sourceError, setSourceError] = useState<string | null>(null);
  const [preview, setPreview] = useState<PackPreview | null>(null);
  const [trustAck, setTrustAck] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [installed, setInstalled] = useState<{ doc: PackAddDoc; notes: string[] } | null>(null);
  const [applying, setApplying] = useState(false);
  // A 409 from the install itself (upstream changed between preview and
  // add, or a daemon whose preview omitted blocking) carries the server's
  // findings; adopting them re-arms the trust gate instead of dead-ending.
  const [serverFindings, setServerFindings] = useState<PackPreviewResource[] | null>(null);

  const handlePreview = useCallback(async () => {
    if (!repo.trim() || loading) return;
    setLoading(true);
    setSourceError(null);
    try {
      const p = await previewPack({
        repo: repo.trim(),
        ref: ref.trim() || undefined,
        path: path.trim() || undefined,
      });
      setPreview(p);
      setTrustAck(false);
      setServerFindings(null);
      setStep('review');
    } catch (err) {
      setSourceError(err instanceof Error ? err.message : 'Preview failed');
    } finally {
      setLoading(false);
    }
  }, [repo, ref, path, loading]);

  const previewFlagged: PackPreviewResource[] = preview
    ? [...preview.skills, ...preview.agents, ...preview.rules].filter(
        (r) => (r.findings ?? []).length > 0,
      )
    : [];
  const flagged = serverFindings ?? previewFlagged;
  // Only blocking findings gate the import (the importer's own severity
  // policy); lower-severity findings stay visible without an ack. A
  // server-side refusal is blocking by definition.
  const needsTrust = serverFindings !== null || flagged.some((r) => r.blocking);

  const handleInstall = useCallback(async () => {
    if (!preview || installing) return;
    setInstalling(true);
    try {
      const res = await addPack({
        repo: repo.trim(),
        ref: ref.trim() || undefined,
        path: path.trim() || undefined,
        trust: needsTrust && trustAck,
      });
      setInstalled(res);
      setStep('done');
    } catch (err) {
      if (err instanceof PackFindingsError) {
        setServerFindings(err.findings);
        setTrustAck(false);
        showToast('error', err.message);
      } else {
        showToast('error', err instanceof Error ? err.message : 'Import failed');
      }
    } finally {
      setInstalling(false);
    }
  }, [preview, installing, repo, ref, path, needsTrust, trustAck]);

  const goToPack = useCallback(
    (packName: string) => {
      close();
      navigate(`/library?kind=pack&selected=${encodeURIComponent(packName)}`);
    },
    [close, navigate],
  );

  const handleApplyNow = useCallback(async () => {
    if (!installed || applying) return;
    setApplying(true);
    try {
      const doc = await applyPack(installed.doc.pack);
      const outcome = describeApplyDoc(doc);
      showToast(outcome.kind, outcome.message);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Apply failed');
    } finally {
      setApplying(false);
      goToPack(installed.doc.pack);
    }
  }, [installed, applying, goToPack]);

  if (step === 'source') {
    return (
      <div className="max-w-lg mx-auto flex flex-col gap-4 py-2">
        <div className="flex items-start gap-3">
          <div className="p-3 rounded-xl bg-surface-elevated/50 border border-border/30">
            <Package size={20} className="text-secondary" />
          </div>
          <div>
            <p className="text-sm text-text-primary font-medium">Import a pack</p>
            <p className="text-xs text-text-muted mt-1">
              A pack repository carries a <span className="font-mono">gridctl-pack.yaml</span>{' '}
              manifest selecting skills, agents, rules, and wiring as one unit. The
              manifest decides the selection; the next step shows exactly what it resolves
              to before anything imports.
            </p>
          </div>
        </div>

        <label className="flex flex-col gap-1 text-xs text-text-muted">
          Repository URL
          <input
            value={repo}
            onChange={(e) => setRepo(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && void handlePreview()}
            placeholder="https://github.com/acme/team-pack"
            aria-label="Pack repository URL"
            className="bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-sm font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50"
          />
        </label>
        <div className="flex gap-3">
          <label className="flex flex-col gap-1 text-xs text-text-muted flex-1">
            Ref (optional)
            <input
              value={ref}
              onChange={(e) => setRef(e.target.value)}
              placeholder="main"
              aria-label="Git ref"
              className="bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-sm font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50"
            />
          </label>
          <label className="flex flex-col gap-1 text-xs text-text-muted flex-1">
            Path (optional)
            <input
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder="subdir"
              aria-label="Repository subdirectory"
              className="bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-sm font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50"
            />
          </label>
        </div>

        {sourceError && (
          <p role="alert" className="text-xs text-status-error">
            {sourceError}
          </p>
        )}

        <div className="flex justify-end">
          <Button onClick={() => void handlePreview()} disabled={!repo.trim() || loading}>
            {loading ? <Loader2 size={14} className="animate-spin" /> : <Download size={14} />}
            {loading ? 'Resolving…' : 'Preview pack'}
          </Button>
        </div>
      </div>
    );
  }

  if (step === 'review' && preview) {
    return (
      <div className="max-w-lg mx-auto flex flex-col gap-4 py-2">
        <div>
          <p className="text-sm text-text-primary font-medium flex items-center gap-2">
            <Package size={14} className="text-secondary" /> {preview.pack}
            {preview.version && (
              <span className="text-[10px] font-mono text-text-muted">v{preview.version}</span>
            )}
          </p>
          {preview.description && (
            <p className="text-xs text-text-muted mt-1">{preview.description}</p>
          )}
          <p className="text-[11px] text-text-muted mt-1.5">
            The manifest selects these resources; there is nothing to pick. Import brings
            them into the registry, and Apply projects them to clients afterward.
          </p>
        </div>

        <ResolvedKind label="Skills" resources={preview.skills} />
        <ResolvedKind label="Agents" resources={preview.agents} />
        <ResolvedKind label="Rules" resources={preview.rules} />
        {preview.wiring && (
          <p className="text-[11px] text-text-muted">
            Includes gateway wiring
            {(preview.clients ?? []).length > 0
              ? ` for ${(preview.clients ?? []).join(', ')}`
              : ' for every detected client'}
            .
          </p>
        )}
        {(preview.unresolved ?? []).length > 0 && (
          <div className="rounded-lg border border-status-pending/30 bg-status-pending/5 p-2.5 text-[11px] text-text-muted">
            <p className="text-status-pending font-medium mb-1">
              {(preview.unresolved ?? []).length} unresolved
            </p>
            <p>
              The manifest selects{' '}
              <span className="font-mono">{(preview.unresolved ?? []).join(', ')}</span> but the
              repository does not ship {(preview.unresolved ?? []).length === 1 ? 'it' : 'them'}.
              The rest of the pack still imports.
            </p>
          </div>
        )}

        {flagged.length > 0 && (
          <div
            role="group"
            aria-label="Security findings"
            className="rounded-lg border border-status-pending/30 bg-status-pending/5 p-3 space-y-2"
          >
            <p className="flex items-center gap-1.5 text-xs text-status-pending font-medium">
              <AlertTriangle size={13} aria-hidden="true" /> Security findings
            </p>
            <ul className="text-[11px] text-text-muted space-y-1.5" aria-live="polite">
              {flagged.map((r) => (
                <li key={`${r.kind}:${r.name}`}>
                  <span className="font-mono text-text-secondary">
                    {r.kind}/{r.name}
                  </span>
                  {(r.findings ?? []).map((f, i) => (
                    <p key={i} className="ml-3">
                      {f.description ?? f.pattern ?? 'flagged content'}
                    </p>
                  ))}
                </li>
              ))}
            </ul>
            {needsTrust ? (
              <label htmlFor={trustId} className="flex items-start gap-2 cursor-pointer text-xs text-text-secondary">
                <input
                  id={trustId}
                  type="checkbox"
                  checked={trustAck}
                  onChange={(e) => setTrustAck(e.target.checked)}
                  className="mt-0.5"
                />
                <span>
                  Import this pack anyway. One acknowledgment covers the whole pack, matching
                  the CLI's single trust gate.
                </span>
              </label>
            ) : (
              <p className="text-[11px] text-text-muted">
                These findings do not block the import; they stay visible so nothing installs
                unseen.
              </p>
            )}
          </div>
        )}

        <div className="flex justify-between">
          <Button variant="ghost" onClick={() => setStep('source')} disabled={installing}>
            <ArrowLeft size={14} /> Back
          </Button>
          <Button
            onClick={() => void handleInstall()}
            disabled={installing || (needsTrust && !trustAck)}
          >
            {installing ? <Loader2 size={14} className="animate-spin" /> : <Download size={14} />}
            {installing ? 'Importing…' : 'Import pack'}
          </Button>
        </div>
      </div>
    );
  }

  if (step === 'done' && installed) {
    const skipped = installed.doc.skipped ?? [];
    const unresolved = installed.doc.unresolved ?? [];
    const clean = skipped.length === 0 && unresolved.length === 0;
    return (
      <div className="max-w-lg mx-auto flex flex-col gap-4 py-4 items-center text-center">
        {clean ? (
          <CheckCircle2 size={28} className="text-status-running" />
        ) : (
          <AlertTriangle size={28} className="text-status-pending" />
        )}
        <p className="text-sm text-text-primary font-medium">
          {clean ? 'Pack imported' : 'Pack partially imported'}
        </p>
        <p className="text-xs text-text-muted max-w-sm">
          Imported into the registry. Apply projects skills, agents, rules, and wiring to
          your clients.
        </p>
        {installed.notes.map((n, i) => (
          <p key={i} className="text-[11px] text-text-muted">{n}</p>
        ))}
        {skipped.length > 0 && (
          <ul className="text-[11px] text-text-muted text-left max-w-sm space-y-1">
            {skipped.map((s, i) => (
              <li key={i}>Skipped: {s}</li>
            ))}
          </ul>
        )}
        {unresolved.length > 0 && (
          <p className="text-[11px] text-status-pending">
            Unresolved: {unresolved.join(', ')}
          </p>
        )}
        <div className="flex items-center gap-2">
          <button
            onClick={() => void handleApplyNow()}
            disabled={applying}
            aria-busy={applying}
            className={cn(
              'px-4 py-2 text-xs font-medium rounded-lg transition-colors',
              'bg-primary text-background hover:bg-primary/90 disabled:opacity-50',
            )}
          >
            {applying ? 'Applying…' : 'Apply now'}
          </button>
          <button
            onClick={() => goToPack(installed.doc.pack)}
            className="px-4 py-2 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors"
          >
            View pack
          </button>
        </div>
      </div>
    );
  }

  return null;
}

/** One kind's resolved names, read-only. */
function ResolvedKind({ label, resources }: { label: string; resources: PackPreviewResource[] }) {
  if (resources.length === 0) return null;
  return (
    <div>
      <p className="text-[10px] uppercase tracking-wider font-medium text-text-muted mb-1">
        {label} <span className="font-mono normal-case">({resources.length})</span>
      </p>
      <p className="text-[11px] font-mono text-text-secondary">
        {resources.map((r) => r.name).join(', ')}
      </p>
    </div>
  );
}
