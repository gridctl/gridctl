import { KeyRound } from 'lucide-react';
import { cn } from '../../lib/cn';
import { openServerInspector, useStackStore } from '../../stores/useStackStore';

/**
 * AuthPendingBadge is the status-bar chip for servers waiting on downstream
 * OAuth authorization. Hidden when nothing is pending; clicking opens the
 * Stack inspector (with the Authorize button) on the first pending server,
 * from any workspace. Follows the PinDriftBadge pattern.
 */
export function AuthPendingBadge() {
  const mcpServers = useStackStore((s) => s.mcpServers);

  const pending = mcpServers.filter((s) => s.authStatus === 'needs_auth');
  if (pending.length === 0) return null;

  const label = `Auth: ${pending.length} pending`;

  const handleClick = () => openServerInspector(pending[0].name);

  return (
    <button
      onClick={handleClick}
      aria-label={`Authorization: ${pending.length} pending. Go to ${pending[0].name}`}
      title={label}
      className={cn(
        'flex items-center gap-2 min-w-0 transition-colors hover:opacity-80',
        'text-status-pending'
      )}
    >
      <KeyRound size={11} className="shrink-0" />
      <span className="relative flex items-center gap-1.5 min-w-0">
        <span className="w-1.5 h-1.5 rounded-full shrink-0 bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]" />
        <span className="font-medium truncate">{label}</span>
      </span>
    </button>
  );
}
