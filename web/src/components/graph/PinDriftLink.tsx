import { useNavigate } from 'react-router';
import { LockOpen } from 'lucide-react';

// PinDriftLink is the full-card drift annotation on Stack canvas server
// nodes, a deep link into the Pins workspace's drift panel. Lives in its own
// module so useNavigate (router context) is only required when a drift is
// actually rendered, keeping router-free CustomNode renders (and tests)
// working. `nodrag` keeps the click a clean activation instead of a canvas
// drag start.
export function PinDriftLink({ serverName }: { serverName: string }) {
  const navigate = useNavigate();
  return (
    <button
      onClick={(e) => {
        e.stopPropagation();
        navigate(`/pins?server=${encodeURIComponent(serverName)}&view=drift`);
      }}
      title="Review the drift diff in the Pins workspace"
      className="nodrag w-full flex items-center gap-1.5 px-2 py-1.5 rounded-md bg-status-pending/5 border border-status-pending/15 hover:bg-status-pending/10 hover:border-status-pending/30 transition-colors cursor-pointer text-left"
    >
      <LockOpen size={11} className="text-status-pending flex-shrink-0" />
      <span className="text-xs text-status-pending/80 font-mono truncate">
        Schema drift detected
      </span>
    </button>
  );
}
