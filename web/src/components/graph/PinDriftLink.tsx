import { useGuardedNavigate } from '../../hooks/useGuardedNavigate';
import { LockOpen } from 'lucide-react';
import type { MouseEvent } from 'react';

// PinDriftLink is the drift annotation on Stack canvas server nodes, a deep
// link into the Pins workspace's drift panel: a strip on full cards and a
// 20 px icon button in the compact header. Lives in its own module so the
// navigate hook (router context) is only required when a drift is actually
// rendered, keeping router-free CustomNode renders (and tests) working.
// `nodrag` keeps the click a clean activation instead of a canvas drag start.
export function PinDriftLink({ serverName, compact = false }: { serverName: string; compact?: boolean }) {
  const navigate = useGuardedNavigate();
  const onClick = (e: MouseEvent) => {
    e.stopPropagation();
    navigate(`/pins?server=${encodeURIComponent(serverName)}&view=drift`);
  };
  const title = 'Review the drift diff in the Pins workspace';
  if (compact) {
    return (
      <button
        type="button"
        onClick={onClick}
        title={title}
        aria-label="Schema drift detected, review in Pins"
        data-glyph="drift"
        className="nodrag flex-shrink-0 flex items-center justify-center w-5 h-5 rounded-md text-status-pending hover:bg-status-pending/10 transition-colors cursor-pointer"
      >
        <LockOpen size={12} aria-hidden="true" />
      </button>
    );
  }
  return (
    <button
      onClick={onClick}
      title={title}
      className="nodrag w-full flex items-center gap-1.5 px-2 py-1.5 rounded-md bg-status-pending/5 border border-status-pending/15 hover:bg-status-pending/10 hover:border-status-pending/30 transition-colors cursor-pointer text-left"
    >
      <LockOpen size={11} className="text-status-pending flex-shrink-0" />
      <span className="text-xs text-status-pending/80 font-mono truncate">
        Schema drift detected
      </span>
    </button>
  );
}
