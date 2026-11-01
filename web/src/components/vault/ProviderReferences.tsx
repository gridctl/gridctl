import { useEffect, useState } from 'react';
import { ConsumerList } from './ConsumerList';
import { consumerCount } from './consumerHelpers';
import { fetchSecrets, type SecretReferenceInfo, type SecretsResponse } from '../../lib/api';
import { cn } from '../../lib/cn';

function statusClass(status: SecretReferenceInfo['status']): string {
  if (status === 'resolved') return 'text-status-running bg-status-running/10';
  if (status === 'error') return 'text-status-error bg-status-error/10';
  return 'text-text-muted bg-surface-elevated';
}

export function ProviderReferences() {
  const [data, setData] = useState<SecretsResponse | null>(null);
  useEffect(() => {
    let cancelled = false;
    fetchSecrets()
      .then((next) => {
        if (!cancelled) setData(next);
      })
      .catch(() => {
        if (!cancelled) setData({ providers: [], references: [] });
      });
    return () => {
      cancelled = true;
    };
  }, []);
  if (!data || data.providers.length === 0) return null;
  return <ProviderReferenceSection providers={data.providers} references={data.references} />;
}

export function ProviderReferenceSection({
  providers = [],
  references,
}: {
  providers?: { name: string; type: string }[];
  references: SecretReferenceInfo[];
}) {
  const [selected, setSelected] = useState<string | null>(null);
  const current = references.find((ref) => ref.key === selected) ?? null;
  return (
    <section aria-label="Provider references" className="px-6 pb-6">
      <h2 className="text-3xs uppercase tracking-[0.24em] text-text-muted/50 mb-2">
        Provider references
      </h2>
      <div role="listbox" aria-label="Provider references">
        {references.map((ref) => (
          <button
            key={ref.key}
            type="button"
            role="option"
            aria-selected={ref.key === selected}
            onClick={() => setSelected(ref.key)}
            className="w-full text-left px-3 py-2 border-b border-border-subtle flex items-center gap-3"
          >
            <span className="font-mono text-xs text-text-primary truncate">{ref.key}</span>
            <span className="text-3xs uppercase tracking-wider px-1.5 py-0.5 rounded bg-surface-elevated text-text-muted">
              {ref.provider}
            </span>
            <span className="text-3xs text-text-muted">{consumerCount(ref.consumers)}</span>
            <span
              className={cn('ml-auto text-3xs uppercase tracking-wider px-1.5 py-0.5 rounded', statusClass(ref.status))}
              title={ref.error ?? undefined}
            >
              {ref.status}
            </span>
          </button>
        ))}
      </div>
      {current && (
        <div aria-label="Provider reference inspector" className="mt-3 rounded-lg border border-border-subtle p-3 text-xs space-y-2">
          <p>Provider: {current.provider}</p>
          <p>Type: {providers.find((item) => item.name === current.provider)?.type ?? ''}</p>
          <p>Path: {current.path}</p>
          <p>Status: {current.status}</p>
          {current.error && <p>Error: {current.error}</p>}
          {current.last_resolved && <p>Last resolved: {current.last_resolved}</p>}
          <ConsumerList consumers={current.consumers} previewLimit={null} />
        </div>
      )}
    </section>
  );
}
