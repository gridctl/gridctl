import { FindingsList } from '../../pins/PinFindings';
import { escapeNonPrintable } from '../../../lib/nonPrintable';
import { packFindingsToPinFindings } from '../../../lib/pinFindings';
import type { PackPreviewResource } from '../../../lib/api';

/**
 * Every security finding on a pack's flagged resources, grouped by resource,
 * so a trust acknowledgment is never asked for content the reviewer was not
 * shown. Resource names and finding text come from the pack and are escaped.
 * The block scrolls so a heavily flagged pack cannot push the actions off screen.
 */
export function PackFindings({ resources }: { resources: PackPreviewResource[] }) {
  const flagged = resources.filter((r) => (r.findings ?? []).length > 0);
  if (flagged.length === 0) return null;
  return (
    <div className="max-h-64 overflow-y-auto scrollbar-dark space-y-2 pr-1">
      {flagged.map((r) => {
        const ref = `${escapeNonPrintable(r.kind)}/${escapeNonPrintable(r.name)}`;
        return (
          <div key={`${r.source ?? ''}:${r.kind}:${r.name}`} className="space-y-1">
            <p className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-2xs">
              <span className="font-mono text-text-secondary break-all">{ref}</span>
              {r.source && (
                <span className="text-3xs text-text-muted">from {escapeNonPrintable(r.source)}</span>
              )}
              {r.blocking && (
                <span className="rounded border border-status-error/30 bg-status-error/10 px-1 text-3xs font-medium text-status-error">
                  blocks import
                </span>
              )}
            </p>
            <FindingsList findings={packFindingsToPinFindings(r.findings)} label={`Security findings on ${ref}`} />
          </div>
        );
      })}
    </div>
  );
}
