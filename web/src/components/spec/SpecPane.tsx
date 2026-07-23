import { SlideOver } from '../ui/SlideOver';
import { SpecTab } from './SpecTab';

interface SpecPaneProps {
  onClose: () => void;
}

// Right-anchored slide-over hosting the spec view inside the Stack workspace
// (the spec IS the stack definition, so it lives with the canvas). Built on
// the shared SlideOver like AccessLens and the pricing manager, so the canvas
// stays live beside it and Escape closes it. Opened via /stack?spec=1 so the
// status-bar chip and palette command deep-link it.
export function SpecPane({ onClose }: SpecPaneProps) {
  return (
    <SlideOver isOpen onClose={onClose} title="Spec" widthClass="w-[620px] max-w-[85%]" closeLabel="Close spec pane">
      <div className="h-full">
        <SpecTab />
      </div>
    </SlideOver>
  );
}
