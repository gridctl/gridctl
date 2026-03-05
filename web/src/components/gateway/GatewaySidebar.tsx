import { memo } from 'react';
import { Activity, X } from 'lucide-react';
import { RegistrySidebar } from '../registry/RegistrySidebar';
import { PopoutButton } from '../ui/PopoutButton';
import { useSelectedNodeData } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import type { GatewayNodeData } from '../../types';

interface GatewaySidebarProps {
  onClose: () => void;
}

const GatewaySidebar = memo(({ onClose }: GatewaySidebarProps) => {
  const selectedData = useSelectedNodeData() as GatewayNodeData | null;
  const registryDetached = useUIStore((s) => s.registryDetached);
  const { openDetachedWindow } = useWindowManager();

  const handlePopout = () => {
    openDetachedWindow('registry');
  };

  return (
    <div className="h-full w-full flex flex-col overflow-hidden">
      {/* Accent line */}
      <div className="absolute top-0 left-0 bottom-0 w-px bg-gradient-to-b from-primary/40 via-primary/20 to-transparent" />

      {/* Gateway header */}
      <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
        <div className="flex items-center gap-3 min-w-0">
          <div className="p-2 rounded-xl flex-shrink-0 border bg-primary/10 border-primary/20">
            <Activity size={16} className="text-primary" />
          </div>
          <div className="min-w-0">
            <h2 className="font-semibold text-text-primary truncate tracking-tight">
              {selectedData?.name ?? 'Gateway'}
            </h2>
            <p className="text-[10px] text-text-muted font-mono tracking-wider">
              {selectedData?.version ?? ''}
            </p>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <PopoutButton
            onClick={handlePopout}
            disabled={registryDetached}
          />
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group">
            <X size={16} className="text-text-muted group-hover:text-text-primary transition-colors" />
          </button>
        </div>
      </div>

      {/* Registry section */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        <RegistrySidebar embedded />
      </div>
    </div>
  );
});

GatewaySidebar.displayName = 'GatewaySidebar';

export { GatewaySidebar };
