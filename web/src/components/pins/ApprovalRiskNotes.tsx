import { escapeNonPrintable } from '../../lib/nonPrintable';
import type { ApprovalRisk } from './pinApproval';

// ApprovalRiskNotes is the shared body of every approve confirmation: one
// line per condition that triggered it, worded the same on every surface.
// fullDiffCommand names the CLI command that prints the untruncated diff.
export function ApprovalRiskNotes({
  risk,
  fullDiffCommand,
}: {
  risk: ApprovalRisk;
  fullDiffCommand?: string;
}) {
  return (
    <>
      {risk.alertFindings > 0 && (
        <p className="text-status-pending">
          The new content carries {risk.alertFindings} warn-or-critical scan{' '}
          {risk.alertFindings === 1 ? 'finding' : 'findings'}. Review{' '}
          {risk.alertFindings === 1 ? 'it' : 'them'} before approving.
        </p>
      )}
      {risk.truncated && (
        <p className="text-status-pending">
          The diff shown is truncated, so part of the change is not visible here.
          {fullDiffCommand && (
            <>
              {' '}Run{' '}
              <code className="font-mono">{escapeNonPrintable(fullDiffCommand)}</code> for the
              full diff.
            </>
          )}
        </p>
      )}
      {risk.large && <p>This is a large change.</p>}
    </>
  );
}
