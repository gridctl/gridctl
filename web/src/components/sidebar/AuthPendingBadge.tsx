import { KeyRound } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';

/**
 * AuthPendingBadge is the status-bar chip for servers waiting on downstream
 * OAuth authorization. Hidden when nothing is pending; clicking selects the
 * first pending server so its sidebar (with the Authorize button) opens.
 * Follows the PinDriftBadge pattern.
 */
export function AuthPendingBadge() {
  const mcpServers = useStackStore((s) => s.mcpServers);
  const selectNode = useStackStore((s) => s.selectNode);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);

  const pending = mcpServers.filter((s) => s.authStatus === 'needs_auth');
  if (pending.length === 0) return null;

  const handleClick = () => {
    selectNode(`mcp-${pending[0].name}`);
    setSidebarOpen(true);
  };

  return (
    <button
      onClick={handleClick}
      aria-label={`Authorization: ${pending.length} pending. Go to ${pending[0].name}`}
      className={cn(
        'flex items-center gap-2 transition-colors hover:opacity-80',
        'text-status-pending'
      )}
    >
      <KeyRound size={11} />
      <span className="relative flex items-center gap-1.5">
        <span className="w-1.5 h-1.5 rounded-full bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]" />
        <span className="font-medium">{`Auth: ${pending.length} pending`}</span>
      </span>
    </button>
  );
}
