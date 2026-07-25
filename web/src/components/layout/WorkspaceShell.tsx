import { useCallback, useEffect, useMemo } from 'react';
import {
  Group,
  Panel,
  Separator,
  usePanelRef,
} from 'react-resizable-panels';
import { cn } from '../../lib/cn';
import { isTextInputTarget } from '../../lib/dom';
import { SeparatorBody } from '../ui/SeparatorBody';
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
          {/* Elevated detail pane: the leftward edge shadow lets it read as
              lifted above the recessed list. Lives on the wrapper so the cast
              shadow is not clipped by the pane's own internal overflow. */}
          <div className="h-full overflow-hidden shadow-pane-left">{right}</div>
        </Panel>
      )}
    </Group>
  );
}

function percentFromPx(px: number, reference = 1440): number {
  return Math.max(5, Math.min(80, (px / reference) * 100));
}
