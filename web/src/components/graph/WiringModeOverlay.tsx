import { useState, useCallback } from 'react';
import { Cable, ArrowRight, Check, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';

interface WiringModeOverlayProps {
  className?: string;
}

interface PendingConnection {
  agentName: string;
  serverName: string;
}

/**
 * Canvas overlay for wiring mode — enables drag connections between
 * agent and server nodes. Creating a connection updates the agent's uses[] array.
 */
export function WiringModeOverlay({ className }: WiringModeOverlayProps) {
  const agents = useStackStore((s) => s.agents);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(null);
  const [pendingConnections, setPendingConnections] = useState<PendingConnection[]>([]);

  const agentUses = agents.reduce<Record<string, string[]>>((acc, agent) => {
    acc[agent.name] = (agent.uses ?? []).map((u) => u.server);
    return acc;
  }, {});

  const handleAgentClick = useCallback((agentName: string) => {
    setSelectedAgent((prev) => (prev === agentName ? null : agentName));
  }, []);

  const handleServerClick = useCallback(
    (serverName: string) => {
      if (!selectedAgent) return;

      const currentUses = agentUses[selectedAgent] ?? [];
      const isConnected = currentUses.includes(serverName);

      if (isConnected) {
        // Remove connection
        setPendingConnections((prev) => [
          ...prev.filter((c) => !(c.agentName === selectedAgent && c.serverName === serverName)),
          { agentName: selectedAgent, serverName: `−${serverName}` },
        ]);
      } else {
        // Add connection
        setPendingConnections((prev) => [
          ...prev.filter((c) => !(c.agentName === selectedAgent && c.serverName === serverName)),
          { agentName: selectedAgent, serverName },
        ]);
      }
    },
    [selectedAgent, agentUses],
  );

  const clearPending = useCallback(() => {
    setPendingConnections([]);
    setSelectedAgent(null);
  }, []);

  return (
    <div className={cn('pointer-events-none', className)}>
      {/* Wiring mode banner */}
      <div className="pointer-events-auto absolute top-3 left-1/2 -translate-x-1/2 z-20">
        <div className="glass-panel rounded-lg px-3 py-1.5 flex items-center gap-2 border border-tertiary/30 bg-tertiary/5">
          <Cable size={12} className="text-tertiary" />
          <span className="text-[10px] font-medium text-tertiary">
            Wiring Mode — {selectedAgent ? `selected: ${selectedAgent}` : 'click an agent to start'}
          </span>
        </div>
      </div>

      {/* Agent selector */}
      <div className="pointer-events-auto absolute top-10 left-3 z-20 space-y-1">
        <div className="glass-panel rounded-lg px-2.5 py-2 border border-tertiary/20">
          <div className="text-[9px] text-tertiary/60 uppercase tracking-wider mb-1.5">Agents</div>
          {agents.map((agent) => {
            const isSelected = selectedAgent === agent.name;
            const uses = agentUses[agent.name] ?? [];
            return (
              <button
                key={agent.name}
                onClick={() => handleAgentClick(agent.name)}
                className={cn(
                  'w-full flex items-center gap-2 px-1.5 py-1 rounded text-left transition-all duration-150',
                  isSelected
                    ? 'bg-tertiary/10 border border-tertiary/30'
                    : 'hover:bg-white/[0.03] border border-transparent',
                )}
              >
                <span
                  className={cn(
                    'text-[10px] font-mono',
                    isSelected ? 'text-tertiary' : 'text-text-muted',
                  )}
                >
                  {agent.name}
                </span>
                <span className="text-[9px] text-text-muted ml-auto">
                  {uses.length} connection{uses.length !== 1 ? 's' : ''}
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {/* Server targets */}
      {selectedAgent && (
        <div className="pointer-events-auto absolute top-10 right-3 z-20 space-y-1">
          <div className="glass-panel rounded-lg px-2.5 py-2 border border-tertiary/20">
            <div className="text-[9px] text-tertiary/60 uppercase tracking-wider mb-1.5">
              Servers (click to wire)
            </div>
            {mcpServers.map((server) => {
              const isConnected = (agentUses[selectedAgent] ?? []).includes(server.name);
              const isPending = pendingConnections.some(
                (c) => c.agentName === selectedAgent && c.serverName === server.name,
              );
              return (
                <button
                  key={server.name}
                  onClick={() => handleServerClick(server.name)}
                  className={cn(
                    'w-full flex items-center gap-2 px-1.5 py-1 rounded text-left transition-all duration-150',
                    isConnected
                      ? 'bg-secondary/10 border border-secondary/30'
                      : isPending
                        ? 'bg-primary/10 border border-primary/30'
                        : 'hover:bg-white/[0.03] border border-transparent',
                  )}
                >
                  {isConnected ? (
                    <Check size={10} className="text-secondary" />
                  ) : (
                    <ArrowRight size={10} className="text-text-muted/40" />
                  )}
                  <span
                    className={cn(
                      'text-[10px] font-mono',
                      isConnected ? 'text-secondary' : 'text-text-muted',
                    )}
                  >
                    {server.name}
                  </span>
                  {isConnected && (
                    <span className="text-[8px] text-secondary/60 ml-auto uppercase tracking-wider">
                      wired
                    </span>
                  )}
                </button>
              );
            })}
          </div>
        </div>
      )}

      {/* Pending changes */}
      {pendingConnections.length > 0 && (
        <div className="pointer-events-auto absolute bottom-14 left-1/2 -translate-x-1/2 z-20">
          <div className="glass-panel rounded-lg px-3 py-2 border border-tertiary/30 flex items-center gap-3">
            <span className="text-[10px] text-tertiary">
              {pendingConnections.length} pending change{pendingConnections.length !== 1 ? 's' : ''}
            </span>
            <button
              onClick={clearPending}
              className="text-[10px] text-text-muted hover:text-text-primary flex items-center gap-1"
            >
              <X size={10} />
              Clear
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
