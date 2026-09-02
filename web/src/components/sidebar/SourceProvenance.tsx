import type { MCPServerSourceStatus } from '../../types';

interface SourceProvenanceProps {
  kind?: string;
  image?: string;
  source?: MCPServerSourceStatus;
  labelClassName?: string;
}

export function SourceProvenance({
  kind,
  image,
  source,
  labelClassName = 'text-sm',
}: SourceProvenanceProps) {
  if (!kind && !image && !source) return null;

  const declared = source?.package
    ? `${source.package}${source.version || source.ref ? `==${source.version || source.ref}` : ''}`
    : source?.url
      ? `${source.url}${source.ref ? `#${source.ref}` : ''}`
      : source?.ref;
  const resolved = source?.commit || source?.artifact;

  return (
    <>
      {kind && <StatusRow label="Kind" value={kind} labelClassName={labelClassName} />}
      {declared && <StatusRow label="Source" value={declared} labelClassName={labelClassName} />}
      {resolved && <StatusRow label="Resolved" value={resolved} labelClassName={labelClassName} />}
      {image && <StatusRow label="Image" value={image} labelClassName={labelClassName} />}
    </>
  );
}

function StatusRow({ label, value, labelClassName }: { label: string; value: string; labelClassName: string }) {
  return (
    <div className="flex justify-between items-center gap-4">
      <span className={`${labelClassName} text-text-muted`}>{label}</span>
      <span className="text-xs text-text-secondary font-mono truncate max-w-[200px] bg-background/50 px-2 py-1 rounded-md" title={value}>
        {value}
      </span>
    </div>
  );
}
