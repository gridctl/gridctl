import type { MCPServerFormData } from '../../../lib/yaml-builder';

export function ExecutionFields({ data, onChange }: {
  data: MCPServerFormData;
  onChange: (patch: Partial<MCPServerFormData>) => void;
}) {
  const execution = data.execution;
  const supported = ['container', 'source', 'local'].includes(data.serverType);
  const patch = (values: Record<string, unknown>) => onChange({ execution: { ...execution, ...values } });
  return (
    <details className="rounded-lg border border-border p-3 space-y-3">
      <summary className="cursor-pointer text-sm font-medium">Execution</summary>
      {!supported && <p className="text-sm">Externally managed execution. Local confinement is unavailable; SSH remote cleanup is unverified.</p>}
      <label className="block text-sm" htmlFor="execution-mode">Execution configuration</label>
      <select id="execution-mode" className="w-full rounded border border-border bg-surface p-2" value={String(execution?.mode ?? '')}
        onChange={(event) => onChange({ execution: event.target.value ? (event.target.value === 'local' ? { mode: 'local', inherit: [], lookup: 'absolute' } : { mode: 'hardened' }) : undefined })}>
        <option value="">Compatibility (no execution requirements)</option>
        {supported && <option value={data.serverType === 'local' ? 'local' : 'hardened'}>{data.serverType === 'local' ? 'Local environment hygiene' : 'Hardened container'}</option>}
        {!supported && execution && <option value={String(execution.mode)}>Inapplicable execution declaration (remove before launch)</option>}
      </select>
      {execution?.mode === 'hardened' && <>
        <p className="text-sm">Requires non-root UID/GID, private PID namespace, nonprivileged operation, finite per-replica limits, and engine-default seccomp. Defaults: no network, read-only root, all capabilities dropped, no new privileges, and bounded scratch. Required unknown evidence blocks routing.</p>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          {['uid', 'gid'].map((field) => <label key={field} className="text-sm">{field.toUpperCase()} (nonzero numeric)
            <input className="block w-full rounded border border-border bg-surface p-2" type="number" min={1} max={4294967295} value={typeof execution[field] === 'number' ? execution[field] : ''}
              onChange={(event) => patch({ [field]: event.target.value ? Number(event.target.value) : 0 })} />
          </label>)}
        </div>
        <label className="block text-sm">Network
          <select className="block w-full rounded border border-border bg-surface p-2" value={String(execution.network ?? 'none')} onChange={(event) => patch({ network: event.target.value })}>
            <option value="none">None (stdio only; no ports or selected network)</option>
            <option value="connected">Connected exception (no destination filtering)</option>
          </select>
        </label>
        <p className="text-sm">Default limits: 256 MiB memory, no swap, one CPU ceiling, 128 PIDs, and 64 MiB nonexecutable /tmp. Data volumes are not storage-bounded. Use YAML mode for explicit limits, mounts, and exceptions.</p>
      </>}
      {data.serverType === 'local' && <p className="text-sm">Unsandboxed local process. Filesystem and network access are not confined. npx/uvx bootstrap runs under the same environment contract.</p>}
      {execution?.mode === 'local' && <>
        <label className="block text-sm">Inherited environment names (empty means none)
          <input className="block w-full rounded border border-border bg-surface p-2" value={Array.isArray(execution.inherit) ? execution.inherit.join(', ') : ''} onChange={(event) => patch({ inherit: event.target.value.split(',').map((name) => name.trim()).filter(Boolean) })} />
        </label>
        <label className="block text-sm">Executable lookup
          <select className="block w-full rounded border border-border bg-surface p-2" value={String(execution.lookup ?? 'absolute')} onChange={(event) => patch({ lookup: event.target.value })}>
            <option value="absolute">Require an absolute executable path</option>
            <option value="ambient_path">Use gateway PATH (explicit exception)</option>
          </select>
        </label>
        <p className="text-sm">Explicit and scoped server delivery takes precedence. Internal credentials remain denied. An absolute path does not authenticate its executable.</p>
      </>}
      {execution && <pre className="overflow-auto whitespace-pre-wrap break-all text-xs" aria-label="Complete requested execution configuration">{JSON.stringify(execution, null, 2)}</pre>}
      <p className="text-sm">Changing or removing execution requirements recreates the workload. Review the authoritative proposed YAML before applying. A saved draft is not enforcement evidence.</p>
    </details>
  );
}
