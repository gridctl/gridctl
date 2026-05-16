import { useCallback, useEffect, useMemo } from 'react';
import {
  Group,
  Panel,
  Separator,
  usePanelRef,
} from 'react-resizable-panels';
import { cn } from '../../lib/cn';
import { useWorkspaceLayout } from '../../hooks/useWorkspaceLayout';
import type { Workspace } from '../../types/workspace';

interface WorkspaceShellProps {
  workspace: Workspace;
  /** Left rail content; omit to render a single-rail layout. */
  left?: React.ReactNode;
  /** Right rail content; omit to render a single-rail layout. */
  right?: React.ReactNode;
  children: React.ReactNode;
  /** Default percentage for the left panel on first mount. */
  defaultLeftPct?: number;
  /** Default percentage for the right panel on first mount. */
  defaultRightPct?: number;
  /** Minimum size (pixels) of the left rail. */
  minLeftPx?: number;
  /** Minimum size (pixels) of the right rail. */
  minRightPx?: number;
  className?: string;
}

const LEFT_ID = 'left';
const CENTER_ID = 'center';
const RIGHT_ID = 'right';

/**
 * WorkspaceShell wraps a workspace surface with resizable left/right rails
 * over react-resizable-panels v4. Widths are persisted per workspace via
 * `useWorkspaceLayout`, double-clicking a separator resets to defaults,
 * and `[` / `]` toggle rail collapse (suppressed when focus is in a
 * text input).
 */
export function WorkspaceShell({
  workspace,
  left,
  right,
  children,
  defaultLeftPct = 18,
  defaultRightPct = 24,
  minLeftPx = 220,
  minRightPx = 300,
  className,
}: WorkspaceShellProps) {
  const leftPanelRef = usePanelRef();
  const rightPanelRef = usePanelRef();

  const panelIds = useMemo(() => {
    const ids: string[] = [];
    if (left) ids.push(LEFT_ID);
    ids.push(CENTER_ID);
    if (right) ids.push(RIGHT_ID);
    return ids;
  }, [left, right]);

  const { defaultLayout, onLayoutChanged } = useWorkspaceLayout({
    workspace,
    key: 'rails',
    panelIds,
  });

  const togglePanel = useCallback(
    (
      ref: React.RefObject<{
        isCollapsed: () => boolean;
        collapse: () => void;
        expand: () => void;
      } | null>,
    ) => {
      const panel = ref.current;
      if (!panel) return;
      if (panel.isCollapsed()) panel.expand();
      else panel.collapse();
    },
    [],
  );

  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if (e.key !== '[' && e.key !== ']') return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isTextInputTarget(e.target)) return;
      if (e.key === '[' && left) {
        e.preventDefault();
        togglePanel(leftPanelRef);
      }
      if (e.key === ']' && right) {
        e.preventDefault();
        togglePanel(rightPanelRef);
      }
    }
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [left, right, togglePanel, leftPanelRef, rightPanelRef]);

  return (
    <Group
      orientation="horizontal"
      className={cn('flex-1 min-h-0 w-full', className)}
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      {left && (
        <Panel
          id={LEFT_ID}
          defaultSize={defaultLeftPct}
          minSize={percentFromPx(minLeftPx)}
          collapsible
          collapsedSize={0}
          panelRef={leftPanelRef}
        >
          <div className="h-full overflow-hidden">{left}</div>
        </Panel>
      )}

      {left && (
        <Separator id="sep-left" className="group/separator relative w-1.5 select-none">
          <SeparatorBody orientation="vertical" />
        </Separator>
      )}

      <Panel
        id={CENTER_ID}
        defaultSize={100 - (left ? defaultLeftPct : 0) - (right ? defaultRightPct : 0)}
        minSize={20}
      >
        <div className="h-full overflow-hidden">{children}</div>
      </Panel>

      {right && (
        <Separator id="sep-right" className="group/separator relative w-1.5 select-none">
          <SeparatorBody orientation="vertical" />
        </Separator>
      )}

      {right && (
        <Panel
          id={RIGHT_ID}
          defaultSize={defaultRightPct}
          minSize={percentFromPx(minRightPx)}
          collapsible
          collapsedSize={0}
          panelRef={rightPanelRef}
        >
          <div className="h-full overflow-hidden">{right}</div>
        </Panel>
      )}
    </Group>
  );
}

interface SeparatorBodyProps {
  orientation: 'vertical' | 'horizontal';
}

/**
 * Visual body of the Separator — mirrors `components/ui/ResizeHandle.tsx`
 * but drops the mouse-event logic since RRP's Separator owns
 * pointer/keyboard interaction.
 */
function SeparatorBody({ orientation }: SeparatorBodyProps) {
  const isVertical = orientation === 'vertical';
  return (
    <div
      className={cn(
        'absolute inset-0 flex items-center justify-center',
        'transition-colors duration-150',
        'group-hover/separator:bg-primary/8',
        'group-data-[separator-active=true]/separator:bg-primary/10',
      )}
    >
      <div
        className={cn(
          'absolute transition-colors duration-150',
          'bg-border/40',
          'group-hover/separator:bg-primary/30',
          'group-data-[separator-active=true]/separator:bg-primary/50',
          isVertical ? 'w-px inset-y-0 left-1/2 -translate-x-px' : 'h-px inset-x-0 top-1/2 -translate-y-px',
        )}
      />
      <div
        className={cn(
          'absolute flex gap-1 transition-all duration-150',
          'opacity-40 group-hover/separator:opacity-100',
          'group-data-[separator-active=true]/separator:opacity-100',
          isVertical ? 'flex-col' : 'flex-row',
        )}
      >
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className={cn(
              'rounded-full transition-all duration-150',
              isVertical ? 'w-0.5 h-1.5' : 'w-1.5 h-0.5',
              'bg-text-muted/30',
              'group-hover/separator:bg-primary/70',
              'group-data-[separator-active=true]/separator:bg-primary',
            )}
          />
        ))}
      </div>
    </div>
  );
}

function percentFromPx(px: number, reference = 1440): number {
  return Math.max(5, Math.min(80, (px / reference) * 100));
}

function isTextInputTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
  if (target.isContentEditable) return true;
  return false;
}
