import { Link } from 'react-router';
import { useGuardedLinkClick } from '../../hooks/useGuardedNavigate';
import { useSecurityReport, type SecurityReportResult } from '../../hooks/useSecurityReport';
import { scopeSecurityChecks, type SecurityScope } from '../../lib/evidenceSummary';
import type { SecurityCheck } from '../../types';

const ALLOWED_ACTION_PREFIX = '/pins';

// Renders the passive security report inside an enclosing section, which
// supplies the title. Pass `state` to share one fetch with a section summary;
// without it the component reads the report itself.
export function SecurityEvidence({ scope, state }: { scope: SecurityScope; state?: SecurityReportResult }) {
  if (state) return <SecurityEvidenceBody scope={scope} state={state} />;
  return <SelfFetchingSecurityEvidence scope={scope} />;
}

function SelfFetchingSecurityEvidence({ scope }: { scope: SecurityScope }) {
  const state = useSecurityReport();
  return <SecurityEvidenceBody scope={scope} state={state} />;
}

function SecurityEvidenceBody({ scope, state }: { scope: SecurityScope; state: SecurityReportResult }) {
  const { report, error, loading, refresh } = state;
  const checks = scopeSecurityChecks(report?.checks ?? [], scope);

  return (
    <section className="space-y-3 max-w-full overflow-x-hidden" aria-label="Security evidence">
      <div className="flex items-start justify-between gap-2 min-w-0">
        <p className="text-xs text-text-muted break-words min-w-0">
          This is a passive report. Refresh re-reads it and does not re-observe downstream evidence. A report with no failures does not mean the server is secure.
        </p>
        <button
          type="button"
          onClick={refresh}
          className="text-xs px-2 py-1 rounded-md border border-border hover:bg-surface-highlight shrink-0"
        >
          Refresh report
        </button>
      </div>
      {loading && <p className="text-sm" role="status">Loading security evidence.</p>}
      {error && <p className="text-sm" role="alert">{error}</p>}
      {report && (
        <p className="text-sm break-words">
          Source {report.source.kind} {report.source.display}. Coverage {report.coverage.status}. Generated {report.generated_at}.
          {report.source.historical ? ' Historical snapshot; not a fresh verification.' : ''}
        </p>
      )}
      {checks.map((check) => <CheckRow key={check.id} check={check} />)}
      {report && !checks.length && !loading && <p className="text-sm">No scoped checks in this report.</p>}
    </section>
  );
}

function CheckRow({ check }: { check: SecurityCheck }) {
  const guardedLinkClick = useGuardedLinkClick();
  const collapsed = check.outcome === 'unknown' || check.outcome === 'not-applicable';
  return (
    <details className="text-sm space-y-1" open={!collapsed}>
      <summary className="cursor-pointer break-words">
        {check.outcome}: {check.predicate} ({check.subject.kind}/{check.subject.name})
      </summary>
      <p className="break-words">{check.explanation}</p>
      <p className="break-words text-xs">
        Basis {check.evidence.basis}; availability {check.evidence.availability}; freshness {check.evidence.freshness}
        {check.evidence.freshness_condition ? ` (${check.evidence.freshness_condition})` : ''}.
      </p>
      {check.evidence.producer && <p className="text-xs break-all" tabIndex={0}>Producer {check.evidence.producer}{check.evidence.producer_version ? ` ${check.evidence.producer_version}` : ''}</p>}
      {check.evidence.ruleset && <p className="text-xs break-all" tabIndex={0}>Ruleset {check.evidence.ruleset}</p>}
      {check.evidence.digest && <p className="text-xs break-all" tabIndex={0}>Digest {check.evidence.digest}</p>}
      {check.evidence.verification_method && <p className="text-xs break-all" tabIndex={0}>Method {check.evidence.verification_method}</p>}
      {check.evidence.subject_binding && <p className="text-xs break-all" tabIndex={0}>Binding {check.evidence.subject_binding}</p>}
      {check.evidence.predicate_scope && <p className="text-xs break-all" tabIndex={0}>Predicate scope {check.evidence.predicate_scope}</p>}
      {check.facts?.instance && <p className="text-xs break-all" tabIndex={0}>Instance {check.facts.instance}</p>}
      {check.facts?.revision && <p className="text-xs break-all" tabIndex={0}>Revision {check.facts.revision}</p>}
      {check.evidence.observed_at && <p className="text-xs">Observed {check.evidence.observed_at}</p>}
      {check.evidence.verified_at && <p className="text-xs">Verified {check.evidence.verified_at}</p>}
      {check.evidence.scanned_at && <p className="text-xs">Scanned {check.evidence.scanned_at}</p>}
      {check.limitations?.map((item) => (
        <p key={item} className="text-xs break-words">{item}</p>
      ))}
      {check.suppression && (
        <p className="text-xs break-words">
          Suppression metadata: {check.suppression.reason_code}
          {check.suppression.codes?.length ? ` (${check.suppression.codes.join(', ')})` : ''}. Not a pass.
        </p>
      )}
      {(check.actions ?? []).map((action) => {
        if (!action.path.startsWith(ALLOWED_ACTION_PREFIX) || action.path.includes('://')) return null;
        return (
          <Link
            key={action.id}
            to={action.path}
            onClick={(e) => guardedLinkClick(e, action.path)}
            className="text-xs underline break-all">
            {action.label}
          </Link>
        );
      })}
    </details>
  );
}
