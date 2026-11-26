import { useCallback, useEffect, useState } from 'react';
import { fetchSecurityReport } from '../lib/api';
import type { SecurityReportState } from '../lib/evidenceSummary';
import type { SecurityReport } from '../types';

const UNAVAILABLE = 'Security evidence report is unavailable.';

export interface SecurityReportResult extends SecurityReportState {
  refresh: () => void;
}

// Reads the passive security report once per mount. Refresh re-reads it; it
// does not re-observe downstream evidence.
export function useSecurityReport(): SecurityReportResult {
  const [report, setReport] = useState<SecurityReport | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

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
        if (!cancelled) applyResult(null, UNAVAILABLE);
      });
    return () => {
      cancelled = true;
    };
  }, [applyResult]);

  const refresh = useCallback(() => {
    setLoading(true);
    setError('');
    fetchSecurityReport()
      .then((next) => applyResult(next, ''))
      .catch(() => applyResult(null, UNAVAILABLE));
  }, [applyResult]);

  return { report, error, loading, refresh };
}
