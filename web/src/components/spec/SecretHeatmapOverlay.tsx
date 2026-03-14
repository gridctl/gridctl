import { useState, useEffect, useMemo } from 'react';
import { KeyRound } from 'lucide-react';
import { cn } from '../../lib/cn';
import { fetchSecretsMap } from '../../lib/api';

interface SecretHeatmapOverlayProps {
  className?: string;
}

// Color palette for secret groups
const SECRET_COLORS = [
  { bg: 'bg-primary/10', border: 'border-primary/30', text: 'text-primary' },
  { bg: 'bg-secondary/10', border: 'border-secondary/30', text: 'text-secondary' },
  { bg: 'bg-tertiary/10', border: 'border-tertiary/30', text: 'text-tertiary' },
  { bg: 'bg-blue-500/10', border: 'border-blue-500/30', text: 'text-blue-400' },
  { bg: 'bg-rose-500/10', border: 'border-rose-500/30', text: 'text-rose-400' },
  { bg: 'bg-emerald-500/10', border: 'border-emerald-500/30', text: 'text-emerald-400' },
];

/**
 * Canvas overlay showing which nodes share vault secrets.
 * Color-coded by secret groups with hover highlighting.
 */
export function SecretHeatmapOverlay({ className }: SecretHeatmapOverlayProps) {
  const [secretsMap, setSecretsMap] = useState<{
    secrets: Record<string, string[]>;
    nodes: Record<string, string[]>;
  } | null>(null);
  const [hoveredSecret, setHoveredSecret] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    fetchSecretsMap()
      .then((data) => { if (!cancelled) setSecretsMap(data); })
      .catch(() => { if (!cancelled) setSecretsMap(null); });
    return () => { cancelled = true; };
  }, []);

  const { sharedSecrets, secretColorMap } = useMemo(() => {
    if (!secretsMap) return { sharedSecrets: [], secretColorMap: new Map<string, number>() };

    // Only show secrets shared by 2+ nodes
    const shared = Object.entries(secretsMap.secrets)
      .filter(([, nodes]) => nodes.length >= 2)
      .sort((a, b) => b[1].length - a[1].length);

    const colorMap = new Map<string, number>();
    shared.forEach(([key], i) => {
      colorMap.set(key, i % SECRET_COLORS.length);
    });

    return { sharedSecrets: shared, secretColorMap: colorMap };
  }, [secretsMap]);

  const highlightedNodes = useMemo(() => {
    if (!hoveredSecret || !secretsMap) return new Set<string>();
    return new Set(secretsMap.secrets[hoveredSecret] ?? []);
  }, [hoveredSecret, secretsMap]);

  if (!secretsMap) {
    return (
      <div className={cn('pointer-events-none', className)}>
        <div className="pointer-events-auto absolute top-3 left-1/2 -translate-x-1/2 z-20">
          <div className="glass-panel rounded-lg px-3 py-1.5 flex items-center gap-2 border border-tertiary/30 bg-tertiary/5">
            <KeyRound size={12} className="text-tertiary" />
            <span className="text-[10px] font-medium text-tertiary">Loading secrets map...</span>
          </div>
        </div>
      </div>
    );
  }

  const totalSecrets = Object.keys(secretsMap.secrets).length;
  const nodesWithSecrets = Object.keys(secretsMap.nodes).length;

  return (
    <div className={cn('pointer-events-none', className)}>
      {/* Heatmap banner */}
      <div className="pointer-events-auto absolute top-3 left-1/2 -translate-x-1/2 z-20">
        <div className="glass-panel rounded-lg px-3 py-1.5 flex items-center gap-2 border border-tertiary/30 bg-tertiary/5">
          <KeyRound size={12} className="text-tertiary" />
          <span className="text-[10px] font-medium text-tertiary">
            Secret Heatmap — {totalSecrets} secret{totalSecrets !== 1 ? 's' : ''} across {nodesWithSecrets} node{nodesWithSecrets !== 1 ? 's' : ''}
          </span>
        </div>
      </div>

      {/* Shared secrets legend */}
      {sharedSecrets.length > 0 && (
        <div className="pointer-events-auto absolute top-10 right-3 z-20 space-y-1">
          <div className="glass-panel rounded-lg px-2.5 py-2 border border-tertiary/20">
            <div className="text-[9px] text-tertiary/60 uppercase tracking-wider mb-1.5">Shared secrets</div>
            {sharedSecrets.slice(0, 10).map(([secret, nodes]) => {
              const colorIdx = secretColorMap.get(secret) ?? 0;
              const color = SECRET_COLORS[colorIdx];
              const isHovered = hoveredSecret === secret;
              return (
                <div
                  key={secret}
                  className={cn(
                    'flex items-center gap-2 px-1.5 py-0.5 rounded cursor-pointer transition-all duration-150',
                    isHovered ? color.bg : 'hover:bg-white/[0.03]',
                  )}
                  onMouseEnter={() => setHoveredSecret(secret)}
                  onMouseLeave={() => setHoveredSecret(null)}
                >
                  <div className={cn('w-1.5 h-1.5 rounded-full', color.bg, color.border, 'border')} />
                  <span className={cn('text-[10px] font-mono', isHovered ? color.text : 'text-text-muted')}>
                    {secret}
                  </span>
                  <span className="text-[9px] text-text-muted ml-auto">
                    {nodes.length} nodes
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      )}

      {/* Per-node secret badges */}
      {Object.entries(secretsMap.nodes).length > 0 && (
        <div className="pointer-events-auto absolute bottom-14 left-3 z-20 space-y-1">
          {Object.entries(secretsMap.nodes).slice(0, 8).map(([node, secrets]) => {
            const isHighlighted = highlightedNodes.has(node);
            return (
              <div
                key={node}
                className={cn(
                  'glass-panel rounded-lg px-2.5 py-1.5 flex items-center gap-2 border transition-all duration-150',
                  isHighlighted ? 'border-tertiary/40 bg-tertiary/10' : 'border-border/20',
                )}
              >
                <KeyRound size={10} className={isHighlighted ? 'text-tertiary' : 'text-text-muted/40'} />
                <span className={cn('text-[10px] font-mono', isHighlighted ? 'text-tertiary' : 'text-text-muted')}>
                  {node}
                </span>
                <span className="text-[9px] text-text-muted">
                  {secrets.length} secret{secrets.length !== 1 ? 's' : ''}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
