import { Navigate } from 'react-router-dom';
import { useRegistryStore } from '../../stores/useRegistryStore';
import { useStackStore } from '../../stores/useStackStore';
import { resolveLandingWorkspace } from '../../lib/landing-workspace';

export function RootRedirect() {
  const stackId = useStackStore((s) => s.gatewayInfo?.name ?? null);
  const skills = useRegistryStore((s) => s.skills);
  const hasSkills = Array.isArray(skills) && skills.length > 0;

  const target = resolveLandingWorkspace({ stackId, hasSkills });
  return <Navigate to={`/${target}`} replace />;
}
