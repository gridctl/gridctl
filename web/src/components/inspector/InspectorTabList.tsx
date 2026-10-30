import type { ReactNode } from 'react';
import { Tab, Tabs } from '../ui/Tabs';

interface InspectorTabListProps {
  ariaLabel: string;
  children: ReactNode;
  className?: string;
}

/**
 * Inspector chrome over Tabs. Arrow keys, roving tabindex, and selection
 * live on the shared primitive; this wrapper only keeps the call shape
 * inspectors already use.
 */
export function InspectorTabList({ ariaLabel, children, className }: InspectorTabListProps) {
  return (
    <Tabs ariaLabel={ariaLabel} className={className}>
      {children}
    </Tabs>
  );
}

interface InspectorTabButtonProps {
  active: boolean;
  onClick: () => void;
  label: string;
  controls: string;
  /** Optional id so a paired tabpanel can reference it via aria-labelledby. */
  id?: string;
}

export function InspectorTabButton({
  active,
  onClick,
  label,
  controls,
  id,
}: InspectorTabButtonProps) {
  return (
    <Tab id={id} selected={active} controls={controls} onSelect={onClick}>
      {label}
    </Tab>
  );
}
