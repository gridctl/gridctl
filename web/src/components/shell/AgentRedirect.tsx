import { Navigate, useLocation } from 'react-router-dom';

// Permanent client-side redirect from /agent → /skills. Query params and
// hash MUST be preserved so existing deep links (e.g. /agent?skill=foo)
// continue to work after the migration.
export function AgentRedirect() {
  const { search, hash } = useLocation();
  return <Navigate to={`/skills${search}${hash}`} replace />;
}
