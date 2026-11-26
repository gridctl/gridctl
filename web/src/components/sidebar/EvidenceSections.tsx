import { Container, ShieldCheck } from 'lucide-react';
import { InspectorSection } from '../inspector';
import { useSecurityReport } from '../../hooks/useSecurityReport';
import { summarizeExecution, summarizeSecurityChecks } from '../../lib/evidenceSummary';
import { SecurityEvidence } from './SecurityEvidence';
import { ExecutionDetails } from './ExecutionDetails';
import type { MCPServerStatus } from '../../types';

// Collapsed per-server security evidence. The summary and the body read one
// report fetch per mount; callers key this by node so a new selection
// starts collapsed and reads the report again.
export function SecurityEvidenceSection({ server }: { server: string }) {
  const state = useSecurityReport();
  const scope = { server };
  return (
    <InspectorSection
      title="Security evidence"
      icon={ShieldCheck}
      summary={summarizeSecurityChecks(state, scope)}
    >
      <SecurityEvidence scope={scope} state={state} />
    </InspectorSection>
  );
}

// Collapsed execution evidence for an MCP server or a resource. Callers key
// this by node so a new selection starts collapsed.
export function ExecutionEvidenceSection({
  server,
  resource = false,
}: {
  server?: MCPServerStatus;
  resource?: boolean;
}) {
  return (
    <InspectorSection
      title="Execution evidence"
      icon={Container}
      summary={summarizeExecution(server, resource)}
    >
      <ExecutionDetails server={server} resource={resource} />
    </InspectorSection>
  );
}
