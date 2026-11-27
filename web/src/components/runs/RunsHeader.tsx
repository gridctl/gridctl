import { useRef, useState, type ReactNode } from 'react';
import { Info, RefreshCw, Settings2 } from 'lucide-react';
import type { RunStatusResponse } from '../../lib/api';
import { formatBytes } from '../../lib/format-bytes';
import { Button } from '../ui/Button';
import { IconButton } from '../ui/IconButton';
import { Popover } from '../ui/Popover';

interface RunsHeaderProps {
  status: RunStatusResponse | null;
  statusError: string | null;
  partial: boolean;
  recordingBlurb: string;
  busy: boolean;
  onRefresh: () => void;
  onExport: () => void;
  onEnable: () => void;
  onDisable: () => void;
  /** Retention and caller-label controls; null when they do not apply. */
  settings: ReactNode | null;
  showWipe: boolean;
  onWipe: () => void;
}

// One status line plus a compact action row. The status endpoint reports no
// record count, and its inventory time is a file modification time, so the
// line states the path, stored size, and file count only.
export function RunsHeader({
  status,
  statusError,
  partial,
  recordingBlurb,
  busy,
  onRefresh,
  onExport,
  onEnable,
  onDisable,
  settings,
  showWipe,
  onWipe,
}: RunsHeaderProps) {
  const [aboutOpen, setAboutOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const aboutRef = useRef<HTMLButtonElement>(null);
  const settingsRef = useRef<HTMLButtonElement>(null);
  const loss = lossSummary(status);
  const showSettings = settings != null || showWipe;

  return (
    <div role="group" aria-label="Run recording" className="flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
      <p className="flex flex-wrap items-center gap-x-2 gap-y-1 min-w-0 flex-1">
        <StatusPhrase status={status} statusError={statusError} />
        {loss && (
          <span className="text-status-pending" role="status">
            <Dot />
            {loss.count} {loss.count === 1 ? 'record' : 'records'} not saved ({loss.parts.join(', ')})
          </span>
        )}
        {partial && (
          <span className="text-status-pending" role="status">
            <Dot />
            Some records could not be read
          </span>
        )}
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <IconButton
          icon={RefreshCw}
          ariaLabel="Refresh run records"
          tooltip="Refresh"
          variant="ghost"
          size="sm"
          onClick={onRefresh}
        />
        <Button variant="secondary" size="sm" onClick={onExport} disabled={busy}>
          Export
        </Button>
        {status && !status.enabled && (
          <Button variant="secondary" size="sm" onClick={onEnable}>
            Enable recording
          </Button>
        )}
        {status?.enabled && (
          <Button variant="secondary" size="sm" onClick={onDisable}>
            Disable recording
          </Button>
        )}
        <div className="relative">
          <IconButton
            ref={aboutRef}
            icon={Info}
            ariaLabel="About run records"
            tooltip="About run records"
            variant="ghost"
            size="sm"
            hasPopup="dialog"
            expanded={aboutOpen}
            onClick={() => setAboutOpen((o) => !o)}
          />
          <Popover
            open={aboutOpen}
            onClose={() => setAboutOpen(false)}
            triggerRef={aboutRef}
            aria-label="About run records"
            className="absolute right-0 top-full mt-1 w-80 max-w-[calc(100vw-2rem)] p-3 space-y-2 text-xs text-text-secondary"
          >
            <p>{status?.recordingNote ?? recordingBlurb}</p>
            {status?.privacyNote && <p>{status.privacyNote}</p>}
            {status?.retentionNote && <p>{status.retentionNote}</p>}
          </Popover>
        </div>
        {showSettings && (
          <div className="relative">
            <IconButton
              ref={settingsRef}
              icon={Settings2}
              ariaLabel="Recording settings"
              tooltip="Recording settings"
              variant="ghost"
              size="sm"
              hasPopup="dialog"
              expanded={settingsOpen}
              onClick={() => setSettingsOpen((o) => !o)}
            />
            <Popover
              open={settingsOpen}
              onClose={() => setSettingsOpen(false)}
              triggerRef={settingsRef}
              aria-label="Recording settings"
              className="absolute right-0 top-full mt-1 w-72 max-w-[calc(100vw-2rem)] p-3 space-y-3 text-xs"
            >
              {settings}
              {settings != null && showWipe && <hr className="border-border/40" />}
              {showWipe && (
                <Button
                  variant="outline-danger"
                  size="sm"
                  disabled={busy}
                  onClick={() => {
                    // Close first so the popover's focus return runs before the
                    // confirm takes focus.
                    setSettingsOpen(false);
                    onWipe();
                  }}
                >
                  Wipe history
                </Button>
              )}
            </Popover>
          </div>
        )}
      </div>
    </div>
  );
}

function Dot() {
  return (
    <span aria-hidden="true" className="mr-2 text-text-muted">
      ·
    </span>
  );
}

function StatusPhrase({ status, statusError }: { status: RunStatusResponse | null; statusError: string | null }) {
  if (statusError) {
    return (
      <span className="text-status-pending" role="status">
        Recording status unavailable
      </span>
    );
  }
  if (!status) return <span className="text-text-muted">Checking recording status…</span>;
  const inv = status.inventory;
  if (!status.enabled) {
    const retained = (status.logical_bytes ?? inv?.sizeBytes ?? 0) > 0 || (inv?.fileCount ?? 0) > 0;
    return (
      <span className="text-text-secondary">
        Recording is off
        {retained && (
          <span className="text-text-muted">
            <span aria-hidden="true" className="mx-2">·</span>
            {formatBytes(inv?.sizeBytes ?? 0)} of earlier records kept on disk
          </span>
        )}
      </span>
    );
  }
  if (!status.effective) {
    const health = status.writer_health === 'degraded' || status.writer_health === 'stopped' ? status.writer_health : null;
    return (
      <span className="text-status-error" role="status">
        Recording is on, but records are not being saved{health ? ` (writer ${health})` : ''}
      </span>
    );
  }
  const fileCount = inv?.fileCount ?? 0;
  return (
    <span className="flex items-center min-w-0 text-text-secondary">
      {inv?.path ? (
        <>
          <span className="flex-shrink-0">Recording to&nbsp;</span>
          <span className="font-mono truncate min-w-0 max-w-[32rem] text-text-primary" title={inv.path}>
            {inv.path}
          </span>
        </>
      ) : (
        'Recording is on'
      )}
      {fileCount > 0 && (
        <span className="flex-shrink-0 text-text-muted">
          <span aria-hidden="true" className="mx-2">·</span>
          {formatBytes(inv.sizeBytes)} in {fileCount} {fileCount === 1 ? 'file' : 'files'}
        </span>
      )}
    </span>
  );
}

// Failures that share a reason code with drops are the same loss counted
// twice, so they are skipped.
function lossSummary(status: RunStatusResponse | null): { count: number; parts: string[] } | null {
  if (!status) return null;
  const parts: string[] = [];
  let count = 0;
  for (const [key, n] of Object.entries(status.drops ?? {})) {
    if (n > 0) {
      parts.push(`${key}: ${n}`);
      count += n;
    }
  }
  for (const [key, n] of Object.entries(status.failures ?? {})) {
    if (n > 0 && !(status.drops && key in status.drops)) {
      parts.push(`${key}: ${n}`);
      count += n;
    }
  }
  return parts.length > 0 ? { count, parts } : null;
}
