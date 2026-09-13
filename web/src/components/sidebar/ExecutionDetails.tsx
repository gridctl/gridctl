import type { MCPServerStatus } from '../../types';

export function ExecutionDetails({ server, resource = false }: { server?: MCPServerStatus; resource?: boolean }) {
  const replicas = server?.replicas ?? [];
  const protectedReplicas = replicas.filter((replica) => replica.execution?.mode === 'hardened');
  const eligible = protectedReplicas.filter((replica) => replica.execution?.eligible).length;
  return <details className="rounded-lg border border-border p-3 space-y-3">
    <summary className="cursor-pointer text-sm font-medium">Execution evidence</summary>
    {server?.execution && <p className="break-all text-sm">Desired mode: {server.execution.mode}; revision: {server.execution.revision}; aggregate: {server.execution.outcome}. Required eligibility: {server.execution.eligible ? 'observed for all active replicas' : 'not established for all replicas'}.</p>}
    {resource ? <p className="text-sm">Resource container execution hardening is not covered.</p>
      : server?.ssh || server?.external || server?.openapi ? <p className="text-sm">Externally managed execution. Remote confinement and descendant cleanup are unverified.</p>
        : server?.localProcess || server?.execution?.mode === 'local' ? <p className="text-sm">Unsandboxed local process. Environment hygiene does not confine filesystem or network access.</p>
          : <p className="text-sm">{eligible} of {replicas.length} active replicas have eligible container evidence. MCP health is separate.</p>}
    {!replicas.length && <p className="text-sm">No current active execution evidence. Configured intent is not an observed runtime.</p>}
    {replicas.map((replica) => <section key={replica.replicaId} className="space-y-2 text-sm">
      <h4>Replica {replica.replicaId}: MCP {replica.healthy ? 'healthy' : 'unhealthy'}; execution {replica.execution?.outcome ?? 'unknown'}</h4>
      {replica.execution && <>
        <p className="break-all">Instance: {replica.execution.instance || 'none'}<br />Revision: {replica.execution.revision || 'compatibility'}</p>
        <p>Runtime: {replica.execution.runtime}; daemon rootless: {replica.execution.daemon_rootless}; user namespace: {replica.execution.user_namespace}</p>
        <p>Reported: {replica.execution.observed_at}. Container snapshots refresh at admission, health checks, and dispatch. Local environment metadata reflects launch construction. This is not continuous monitoring.</p>
        <div className="overflow-auto"><table className="w-full text-left text-xs">
          <caption className="text-left">Requested controls and evidence</caption>
          <thead><tr><th scope="col">Control</th><th scope="col">Requested</th><th scope="col">Observed</th><th scope="col">Outcome/source</th></tr></thead>
          <tbody>{(replica.execution.controls ?? []).map((control, index) => <tr key={`${control.field}-${index}`}>
            <th scope="row" className="p-1 align-top">{control.field}</th><td className="p-1 align-top break-all">{control.requested}</td><td className="p-1 align-top">{control.observed ?? 'unknown'}</td><td className="p-1 align-top">{control.outcome}<br />{control.source}</td>
          </tr>)}</tbody>
        </table></div>
        {!replica.execution.controls?.length && <p>No per-control evidence available.</p>}
      </>}
    </section>)}
    <p className="text-xs">Post-start observations leave a window before verification. Network none does not constrain image pulls, build downloads, or information returned through MCP.</p>
  </details>;
}
