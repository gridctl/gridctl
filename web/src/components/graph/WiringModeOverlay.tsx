import { Cable } from 'lucide-react';
import { cn } from '../../lib/cn';

interface WiringModeOverlayProps {
  className?: string;
}

/**
 * Canvas overlay for wiring mode — visual indicator that wiring mode is active.
 */
export function WiringModeOverlay({ className }: WiringModeOverlayProps) {
  return (
    <div className={cn('pointer-events-none', className)}>
      {/* Wiring mode banner */}
      <div className="pointer-events-auto absolute top-3 left-1/2 -translate-x-1/2 z-20">
        <div className="glass-panel rounded-lg px-3 py-1.5 flex items-center gap-2 border border-tertiary/30 bg-tertiary/5">
          <Cable size={12} className="text-tertiary" />
          <span className="text-[10px] font-medium text-tertiary">
            Wiring Mode
          </span>
        </div>
      </div>
    </div>
  );
}
