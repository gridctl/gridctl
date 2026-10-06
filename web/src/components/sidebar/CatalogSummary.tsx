import type { DownstreamCapabilityStatus } from '../../types';

interface CatalogFields {
  promptCount?: number;
  mcpResourceCount?: number;
  resourceTemplateCount?: number;
  capabilities?: DownstreamCapabilityStatus;
  resourceCollisions?: number;
  resourceListError?: string;
}

const badges: { key: keyof DownstreamCapabilityStatus; label: string }[] = [
  { key: 'prompts', label: 'prompts' },
  { key: 'resources', label: 'resources' },
  { key: 'resourcesSubscribe', label: 'subscribe' },
  { key: 'resourcesListChanged', label: 'list changed' },
];

export function CatalogSummary({ data }: { data: CatalogFields }) {
  const caps = data.capabilities;
  const declared = badges.filter((badge) => caps?.[badge.key]);
  return (
    <div className="space-y-2">
      <div className="flex justify-between items-center">
        <span className="text-xs text-text-muted">Prompts</span>
        <span className="text-xs font-mono text-text-secondary">{data.promptCount ?? 0}</span>
      </div>
      <div className="flex justify-between items-center">
        <span className="text-xs text-text-muted" title="MCP resources, not stack infrastructure">
          MCP resources
        </span>
        <span className="text-xs font-mono text-text-secondary">{data.mcpResourceCount ?? 0}</span>
      </div>
      <div className="flex justify-between items-center">
        <span className="text-xs text-text-muted">Templates</span>
        <span className="text-xs font-mono text-text-secondary">{data.resourceTemplateCount ?? 0}</span>
      </div>
      {declared.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {declared.map((badge) => (
            <span
              key={badge.key}
              className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-violet-500/10 text-violet-300"
            >
              {badge.label}
            </span>
          ))}
        </div>
      )}
      {(data.resourceListError || (data.resourceCollisions ?? 0) > 0) && (
        <div className="text-xs text-status-pending bg-status-pending/10 border border-status-pending/20 rounded-md px-2 py-1">
          {data.resourceListError ? `List error: ${data.resourceListError}` : null}
          {data.resourceListError && (data.resourceCollisions ?? 0) > 0 ? ' · ' : null}
          {(data.resourceCollisions ?? 0) > 0 ? `${data.resourceCollisions} URI collision${data.resourceCollisions === 1 ? '' : 's'}` : null}
        </div>
      )}
    </div>
  );
}
