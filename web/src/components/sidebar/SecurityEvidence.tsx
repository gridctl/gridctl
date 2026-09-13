import { useCallback, useEffect, useId, useState } from 'react';
import { Link } from 'react-router';
import { fetchSecurityReport } from '../../lib/api';
import type { SecurityCheck, SecurityReport } from '../../types';

const ALLOWED_ACTION_PREFIX = '/pins';

export function SecurityEvidence({ scope }: { scope: 'gateway' | { server: string } }) {
  const [report, setReport] = useState<SecurityReport | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const statusId = useId();

  const applyResult = useCallback((next: SecurityReport | null, message: string) => {
    setReport(next);
    setError(message);
    setLoading(false);
  }, []);

  useEffect(() => {
    let cancelled = false;
    fetchSecurityReport()
      .then((next) => {
        if (!cancelled) applyResult(next, '');
      })
      .catch(() => {
        if (!cancelled) applyResult(null, 'Security evidence report is unavailable.');
      });
    return () => {
      cancelled = true;
    };
  }, [applyResult]);

  const refresh = () => {
    setLoading(true);
    setError('');
    fetchSecurityReport()
      .then((next) => applyResult(next, ''))
      .catch(() => applyResult(null, 'Security evidence report is unavailable.'));
  };

  const checks = (report?.checks ?? []).filter((check) => {
    if (scope === 'gateway') {
      return check.subject.kind === 'gateway' || check.subject.kind === 'skill';
    }
    return (
      (check.subject.kind === 'server' && check.subject.name === scope.server)
      || (check.subject.kind === 'replica' && check.subject.name.startsWith(`${scope.server}:`))
    );
  });

  return (
    <section className="rounded-lg border border-border p-3 space-y-3" aria-labelledby={statusId}>
      <div className="flex items-center justify-between gap-2">
        <h3 id={statusId} className="text-sm font-medium">
          Security evidence
        </h3>
        <button
          type="button"
          onClick={refresh}
          className="text-xs px-2 py-1 rounded-md border border-border hover:bg-surface-highlight"
        >
          Refresh report
        </button>
      </div>
      <p className="text-xs text-text-muted break-words">
        Refresh re-reads this passive report. It does not re-observe downstream evidence. Exit zero is not secure.
      </p>
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
      {check.evidence.observed_at && <p className="text-xs">Observed {check.evidence.observed_at}</p>}
      {check.evidence.verified_at && <p className="text-xs">Verified {check.evidence.verified_at}</p>}
      {check.limitations?.map((item) => (
        <p key={item} className="text-xs break-words">{item}</p>
      ))}
      {check.suppression && (
        <p className="text-xs break-words">Suppression metadata: {check.suppression.reason_code}. Not a pass.</p>
      )}
      {(check.actions ?? []).map((action) => {
        if (!action.path.startsWith(ALLOWED_ACTION_PREFIX) || action.path.includes('://')) return null;
        return (
          <Link key={action.id} to={action.path} className="text-xs underline break-all">
            {action.label}
          </Link>
        );
      })}
    </details>
  );
}
