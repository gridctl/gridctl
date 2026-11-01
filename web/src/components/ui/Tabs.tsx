import {
  createContext,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  type CSSProperties,
  type KeyboardEvent,
  type ReactNode,
  type Ref,
} from 'react';
import { cn } from '../../lib/cn';

// Selection follows focus (automatic activation), matching the Library skill
// inspector: arrow keys both move focus and call the destination tab's onSelect.

interface TabsContextValue {
  register: (id: string, onSelect: () => void) => void;
  unregister: (id: string) => void;
}

const TabsContext = createContext<TabsContextValue | null>(null);

interface TabsProps {
  /** Accessible name for the tablist. */
  ariaLabel: string;
  children: ReactNode;
  className?: string;
}

const TABLIST_CLASS =
  'px-5 pt-3 flex items-center gap-1 border-b border-border-subtle/50';

export function Tabs({ ariaLabel, children, className }: TabsProps) {
  const ref = useRef<HTMLDivElement>(null);
  const handlers = useRef(new Map<string, () => void>());
  const api = useMemo<TabsContextValue>(() => ({
    register(id, onSelect) {
      handlers.current.set(id, onSelect);
    },
    unregister(id) {
      handlers.current.delete(id);
    },
  }), []);

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft' && e.key !== 'Home' && e.key !== 'End') {
      return;
    }
    if (e.altKey || e.metaKey || e.ctrlKey) return;
    const root = ref.current;
    if (!root) return;
    const tabs = Array.from(root.querySelectorAll<HTMLButtonElement>('[role="tab"]'));
    if (tabs.length === 0) return;

    const fromTarget = tabs.findIndex(
      (tab) => tab === e.target || (e.target instanceof Node && tab.contains(e.target)),
    );
    const focused = tabs.findIndex((tab) => tab === document.activeElement);
    const selected = tabs.findIndex((tab) => tab.getAttribute('aria-selected') === 'true');
    const idx = fromTarget >= 0 ? fromTarget : focused >= 0 ? focused : Math.max(selected, 0);

    let next = idx;
    if (e.key === 'ArrowRight') next = (idx + 1) % tabs.length;
    else if (e.key === 'ArrowLeft') next = (idx - 1 + tabs.length) % tabs.length;
    else if (e.key === 'Home') next = 0;
    else next = tabs.length - 1;

    e.preventDefault();
    const target = tabs[next];
    if (!target) return;
    target.focus();
    handlers.current.get(target.id)?.();
  };

  return (
    <TabsContext.Provider value={api}>
      <div
        ref={ref}
        role="tablist"
        aria-label={ariaLabel}
        onKeyDown={onKeyDown}
        className={cn(TABLIST_CLASS, className)}
      >
        {children}
      </div>
    </TabsContext.Provider>
  );
}

interface TabProps {
  id?: string;
  selected: boolean;
  /** Id of the tabpanel this tab controls. */
  controls: string;
  onSelect: () => void;
  children: ReactNode;
  className?: string;
}

const TAB_CLASS = cn(
  'px-3 py-1.5 -mb-px',
  'font-mono text-3xs uppercase tracking-[0.2em]',
  'border-b-2 transition-colors',
  'focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
);

export function Tab({ id, selected, controls, onSelect, children, className }: TabProps) {
  const autoId = useId();
  const tabId = id ?? autoId;
  const ctx = useContext(TabsContext);

  useEffect(() => {
    if (!ctx) return;
    ctx.register(tabId, onSelect);
    return () => ctx.unregister(tabId);
  }, [ctx, tabId, onSelect]);

  return (
    <button
      type="button"
      id={tabId}
      role="tab"
      aria-selected={selected}
      aria-controls={controls}
      tabIndex={selected ? 0 : -1}
      onClick={onSelect}
      className={cn(
        TAB_CLASS,
        selected
          ? 'border-primary text-text-primary'
          : 'border-transparent text-text-muted hover:text-text-primary',
        className,
      )}
    >
      {children}
    </button>
  );
}

interface TabPanelProps {
  id: string;
  labelledBy: string;
  /** Inactive panels are hidden. Defaults to true (visible). */
  active?: boolean;
  hidden?: boolean;
  className?: string;
  style?: CSSProperties;
  children?: ReactNode;
  panelRef?: Ref<HTMLDivElement>;
}

export function TabPanel({
  id,
  labelledBy,
  active = true,
  hidden,
  className,
  style,
  children,
  panelRef,
}: TabPanelProps) {
  const isHidden = hidden ?? !active;
  return (
    <div
      ref={panelRef}
      role="tabpanel"
      id={id}
      aria-labelledby={labelledBy}
      hidden={isHidden || undefined}
      className={className}
      style={style}
    >
      {children}
    </div>
  );
}
