import { useMemo } from 'react';
import { AlertTriangle, Info } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { ServerType } from '../../lib/yaml-builder';

interface TransportAdvisorProps {
  serverType: ServerType;
  transport: string;
}

interface Advisory {
  level: 'warning' | 'info';
  message: string;
  suggestion?: string;
}

/**
 * Proactive transport compatibility advisor.
 * Shows inline warnings for incompatible transport/server type combinations
 * and suggestions for optimal configurations.
 */
export function TransportAdvisor({ serverType, transport }: TransportAdvisorProps) {
  const advisories = useMemo(() => {
    const items: Advisory[] = [];

    // External URL with stdio — not supported
    if (serverType === 'external' && transport === 'stdio') {
      items.push({
        level: 'warning',
        message: 'stdio transport is not compatible with external URL servers',
        suggestion: 'Use HTTP or SSE transport for external servers',
      });
    }

    // Container with stdio — needs command
    if (serverType === 'container' && transport === 'stdio') {
      items.push({
        level: 'info',
        message: 'stdio transport communicates via stdin/stdout — no port needed',
        suggestion: 'Ensure the container image supports stdio communication',
      });
    }

    // Source build with stdio
    if (serverType === 'source' && transport === 'stdio') {
      items.push({
        level: 'info',
        message: 'stdio transport will bypass port allocation',
      });
    }

    // SSE with external — works but verify server support
    if (serverType === 'external' && transport === 'sse') {
      items.push({
        level: 'info',
        message: 'Ensure the external server supports SSE (Server-Sent Events)',
      });
    }

    // Container with HTTP but no port
    if (serverType === 'container' && transport === 'http') {
      items.push({
        level: 'info',
        message: 'HTTP transport requires a port for the MCP gateway to connect',
      });
    }

    return items;
  }, [serverType, transport]);

  if (advisories.length === 0) return null;

  return (
    <div className="space-y-1 mt-1">
      {advisories.map((advisory, i) => (
        <div
          key={i}
          className={cn(
            'flex items-start gap-2 px-2.5 py-1.5 rounded-lg text-[10px] leading-relaxed',
            advisory.level === 'warning'
              ? 'bg-status-pending/10 border border-status-pending/20 text-status-pending'
              : 'bg-primary/5 border border-primary/10 text-text-muted',
          )}
        >
          {advisory.level === 'warning' ? (
            <AlertTriangle size={10} className="mt-0.5 shrink-0" />
          ) : (
            <Info size={10} className="mt-0.5 shrink-0" />
          )}
          <div>
            <span>{advisory.message}</span>
            {advisory.suggestion && (
              <span className="block mt-0.5 text-text-muted">{advisory.suggestion}</span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
