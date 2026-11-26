import { useId, useState, type ComponentType, type ReactNode } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';

interface InspectorSectionProps {
  title: string;
  icon?: ComponentType<{ size?: number; className?: string }>;
  count?: number;
  defaultOpen?: boolean;
  // Opens the section when it changes to true. It never closes the section,
  // and a section the user collapsed stays collapsed while it remains true.
  autoOpen?: boolean;
  // Second header line, readable while collapsed and while open.
  summary?: ReactNode;
  children: ReactNode;
}

/**
 * Collapsible section used inside the Stack Sidebar, the gateway sidebar,
 * and the detached-sidebar popout. Section headers carry an optional Lucide
 * icon, an optional count badge, and an optional summary line. The panel
 * element is always rendered so the toggle's aria-controls resolves, and it
 * is `hidden` while collapsed.
 * Its contents mount on the first open and stay mounted afterwards, so a
 * section that was never opened does no work, and collapsing keeps state
 * such as an unsaved tool selection.
 */
export function InspectorSection({
  title,
  icon: Icon,
  count,
  defaultOpen = false,
  autoOpen,
  summary,
  children,
}: InspectorSectionProps) {
  const [isOpen, setIsOpen] = useState(defaultOpen);
  const [hasOpened, setHasOpened] = useState(defaultOpen);
  const [prevAutoOpen, setPrevAutoOpen] = useState(autoOpen);
  if (autoOpen !== prevAutoOpen) {
    setPrevAutoOpen(autoOpen);
    if (autoOpen) {
      setIsOpen(true);
      setHasOpened(true);
    }
  }
  const panelId = useId();

  const toggle = () => {
    if (!isOpen) setHasOpened(true);
    setIsOpen(!isOpen);
  };

  return (
    <div className="border-b border-border/30">
      <button
        type="button"
        onClick={toggle}
        aria-expanded={isOpen}
        aria-controls={panelId}
        className="w-full flex items-center justify-between gap-3 p-4 hover:bg-surface-highlight/50 transition-colors group"
      >
        <div className="flex items-center gap-2.5 min-w-0">
          {Icon && (
            <div className="p-1 rounded-md bg-surface-highlight/50 group-hover:bg-surface-highlight transition-colors">
              <Icon
                size={12}
                className="text-text-muted group-hover:text-primary transition-colors"
              />
            </div>
          )}
          <div className="min-w-0 text-left">
            <div className="flex items-center gap-2.5">
              <span className="text-sm font-medium text-text-primary">{title}</span>
              {count !== undefined && (
                <span className="text-3xs text-text-muted bg-surface-elevated px-1.5 py-0.5 rounded-md font-mono">
                  {count}
                </span>
              )}
            </div>
            {summary !== undefined && (
              <span className="block text-2xs text-text-muted text-left break-words">{summary}</span>
            )}
          </div>
        </div>
        <div className="p-1 shrink-0 rounded-md group-hover:bg-surface-highlight transition-colors">
          {isOpen ? (
            <ChevronDown size={14} className="text-text-muted" />
          ) : (
            <ChevronRight size={14} className="text-text-muted" />
          )}
        </div>
      </button>
      <div id={panelId} hidden={!isOpen}>
        {hasOpened && <div className="px-4 pb-4">{children}</div>}
      </div>
    </div>
  );
}
