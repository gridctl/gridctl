import { InspectorValueRow } from '../inspector/InspectorValueRow';
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
      {kind && <InspectorValueRow label="Kind" value={kind} labelClassName={labelClassName} />}
      {declared && (
        <InspectorValueRow label="Source" value={declared} copyLabel="Copy source" labelClassName={labelClassName} />
      )}
      {resolved && (
        <InspectorValueRow label="Resolved" value={resolved} copyLabel="Copy resolved source" labelClassName={labelClassName} />
      )}
      {image && <InspectorValueRow label="Image" value={image} copyLabel="Copy image" labelClassName={labelClassName} />}
    </>
  );
}
