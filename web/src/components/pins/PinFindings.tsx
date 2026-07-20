import { Info, ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { maxFindingSeverity } from '../../lib/pinFindings';
import { severityClasses, severityIcon } from '../../lib/severity';
import type { PinFinding } from '../../lib/api';

// Findings are advisory - amber informs, it never blocks - so nothing here
// disables or gates any action. Severity presentation comes from the shared
// lib/severity maps, the same vocabulary OptimizeSection uses.

// FindingsList renders poisoning-scan findings for one tool. Snippets and
// decoded payloads quote attacker-controlled text and always pass through
// escapeNonPrintable - a hidden-Unicode finding rendered raw would re-hide
// the very payload it flags.
export function FindingsList({ findings }: { findings: PinFinding[] | undefined }) {
  if (!findings || findings.length === 0) return null;
  return (
    <ul className="space-y-1" aria-label="Poisoning scan findings">
      {findings.map((f, i) => {
        const Icon = severityIcon[f.severity] ?? Info;
        return (
          <li
            key={`${f.code}-${f.field}-${i}`}
            className={cn('rounded-md border px-2 py-1.5 text-[11px]', severityClasses[f.severity] ?? severityClasses.info)}
          >
            <div className="flex items-center gap-1.5">
              <Icon size={11} className="flex-shrink-0" />
              <span className="font-mono font-medium">{f.code}</span>
              <span className="text-text-secondary min-w-0 break-words">
                {escapeNonPrintable(f.message)}
              </span>
              <span className="ml-auto flex-shrink-0 text-[10px] text-text-muted">
                {f.field} · {f.confidence} confidence
              </span>
            </div>
            {f.snippet && (
              <div className="mt-1 pl-4 font-mono text-[10px] text-text-secondary whitespace-pre-wrap break-words">
                {escapeNonPrintable(f.snippet)}
              </div>
            )}
            {f.decoded && (
              <div className="mt-1 pl-4 text-[10px] whitespace-pre-wrap break-words">
                <span className="text-status-error font-medium">decoded hidden message: </span>
                <span className="font-mono text-text-secondary">{escapeNonPrintable(f.decoded)}</span>
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}

// FindingsSummaryBadge is the compact per-tool marker for dense layouts
// (currently the pinned records table).
export function FindingsSummaryBadge({ findings }: { findings: PinFinding[] | undefined }) {
  const severity = maxFindingSeverity(findings);
  if (!severity || !findings) return null;
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium border',
        severityClasses[severity],
      )}
    >
      <ShieldAlert size={9} />
      {findings.length} finding{findings.length > 1 ? 's' : ''}
    </span>
  );
}
