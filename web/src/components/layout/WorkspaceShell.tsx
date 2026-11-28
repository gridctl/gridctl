import { useCallback, useEffect, useId, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import {
  Group,
  Panel,
  Separator,
  usePanelRef,
} from 'react-resizable-panels';
import { cn } from '../../lib/cn';
import { isModalDialogOpen, isTextInputTarget } from '../../lib/dom';
import { SeparatorBody } from '../ui/SeparatorBody';
import { useContainerWidth } from '../../hooks/useContainerWidth';
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
  /** Below this shell width (pixels) the left rail becomes a drawer. */
  collapseLeftBelowPx?: number;
  /** Below this shell width (pixels) the right rail becomes a drawer. */
  collapseRightBelowPx?: number;
  /** Identity of the left rail's primary selection. A change closes an open left drawer. */
  leftKey?: string | null;
  /**
   * Identity of what the right rail shows, or null when nothing is selected.
   * In drawer mode a non-null change opens the right drawer and null closes it.
   */
  rightKey?: string | null;
  className?: string;
}

type Side = 'left' | 'right';

type PanelHandle = React.RefObject<{
  isCollapsed: () => boolean;
  collapse: () => void;
  expand: () => void;
} | null>;

type PendingFocus = { side: Side; target: 'toggle' | 'into' | HTMLElement };

const LEFT_ID = 'left';
const CENTER_ID = 'center';
const RIGHT_ID = 'right';

const DRAWER_GUTTER_PX = 56;

// react-resizable-panels widens each separator's hit region to
// resizeTargetMinimumSize, which reaches (24 - 6) / 2 = 9 px past a 6 px
// separator on coarse pointers. A press there starts a drag even over a
// stacked button, so a docked toggle sits clear of it.
const RESIZE_TARGET = { coarse: 24, fine: 16 };
const SEPARATOR_PX = 6;
const DOCKED_TOGGLE_INSET_PX = (RESIZE_TARGET.coarse - SEPARATOR_PX) / 2 + 1;

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex^="-"])',
].join(',');

function createHost(): HTMLDivElement {
  const host = document.createElement('div');
  host.className = 'h-full';
  return host;
}

function containsFocus(host: HTMLElement): boolean {
  const active = document.activeElement;
  return active instanceof Node && host.contains(active);
}

interface RailSlotProps {
  host: HTMLElement;
  /** Element focused inside the host when it last left a slot. */
  focusRef: React.RefObject<HTMLElement | null>;
}

// Adopts the rail's portal host. The host is the portal container for the
// rail content, so moving it between slots re-parents the DOM without
// remounting the React subtree.
function RailSlot({ host, focusRef }: RailSlotProps) {
  const ref = useRef<HTMLDivElement>(null);

  useLayoutEffect(() => {
    const slot = ref.current;
    if (!slot) return;
    if (host.parentNode !== slot) slot.appendChild(host);
    const pending = focusRef.current;
    if (pending && host.contains(pending) && document.activeElement !== pending) {
      pending.focus({ preventScroll: true });
    }
    // A slot inside a hidden drawer cannot take focus; the shell keeps the
    // record and restores it once the drawer opens.
    if (pending && document.activeElement === pending) focusRef.current = null;
    return () => {
      const active = document.activeElement;
      if (active instanceof HTMLElement && host.contains(active)) focusRef.current = active;
      if (host.parentNode === slot) slot.removeChild(host);
    };
  }, [host, focusRef]);

  return <div ref={ref} className="h-full" />;
}

interface RailToggleProps {
  side: Side;
  expanded: boolean;
  controls?: string;
  offsetPx: number;
  /** Detached from the edge (docked beside a separator), so fully rounded. */
  inset?: boolean;
  onToggle: () => void;
  buttonRef: React.RefObject<HTMLButtonElement | null>;
}

function RailToggle({ side, expanded, controls, offsetPx, inset, onToggle, buttonRef }: RailToggleProps) {
  const label = `${expanded ? 'Collapse' : 'Expand'} ${side} rail`;
  const shortcut = side === 'left' ? '[' : ']';
  const pointsLeft = (side === 'left') === expanded;
  const Icon = pointsLeft ? ChevronLeft : ChevronRight;
  return (
    <button
      ref={buttonRef}
      type="button"
      onClick={onToggle}
      aria-label={label}
      title={`${label} (${shortcut})`}
      aria-expanded={expanded}
      aria-controls={controls}
      aria-keyshortcuts={shortcut}
      style={side === 'left' ? { left: offsetPx } : { right: offsetPx }}
      className={cn(
        // Sits below the separator grip dots.
        'absolute z-raised top-[calc(50%+1.75rem)] flex h-10 w-6 items-center justify-center',
        'bg-surface-elevated border border-border text-text-muted',
        'hover:bg-surface-highlight hover:text-text-primary transition-colors',
        'focus-visible:-outline-offset-2',
        inset
          ? 'rounded-md'
          : side === 'left' ? 'rounded-r-md border-l-0' : 'rounded-l-md border-r-0',
      )}
    >
      <Icon size={14} aria-hidden="true" />
    </button>
  );
}

/**
 * WorkspaceShell wraps a workspace surface with resizable left/right rails
 * over react-resizable-panels v4. Widths are persisted per workspace and per
 * set of docked panels via `useWorkspaceLayout`, double-clicking a separator
 * resets to defaults, and `[` / `]` or the visible rail toggles collapse a
 * docked rail (the keys are suppressed when focus is in a text input or
 * while a modal dialog is open).
 *
 * Below `collapseRightBelowPx` (900) of shell width the right rail is not
 * docked, and below `collapseLeftBelowPx` (640) neither is the left rail.
 * An undocked rail opens as a non-modal overlay drawer on its own side
 * through its toggle, its key, or a `rightKey` change; Escape closes it, and
 * a press outside closes the left drawer. Rail content stays mounted in
 * every mode. A shell that has not been measured yet keeps both rails docked.
 *
 * A `rightKey` change opens the drawer one synchronous render after the
 * commit that mounts the new rail content, so `autoFocus` in that content
 * does not take focus in drawer mode (it does when docked). Opening through
 * `rightKey` is not meant to move focus.
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
  collapseLeftBelowPx = 640,
  collapseRightBelowPx = 900,
  leftKey,
  rightKey,
  className,
}: WorkspaceShellProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const { width } = useContainerWidth(rootRef);

  const hasLeft = !!left;
  const hasRight = !!right;
  // Zero width means not measured yet, which must look like a wide window.
  const leftDocked = width === 0 || width >= collapseLeftBelowPx;
  const rightDocked = width === 0 || width >= collapseRightBelowPx;
  const showLeftPanel = hasLeft && leftDocked;
  const showRightPanel = hasRight && rightDocked;
  const leftDrawerWidth = Math.min(Math.max(minLeftPx, 260), width - DRAWER_GUTTER_PX);
  const rightDrawerWidth = Math.min(Math.max(minRightPx, 360), width - DRAWER_GUTTER_PX);

  const leftPanelRef = usePanelRef();
  const rightPanelRef = usePanelRef();
  const [leftCollapsed, setLeftCollapsed] = useState(false);
  const [rightCollapsed, setRightCollapsed] = useState(false);

  // One detached host per rail, used as its portal container for the
  // lifetime of the shell.
  const [leftHost] = useState(createHost);
  const [rightHost] = useState(createHost);
  const leftFocusRef = useRef<HTMLElement | null>(null);
  const rightFocusRef = useRef<HTMLElement | null>(null);
  const leftDrawerRef = useRef<HTMLDivElement>(null);
  const rightDrawerRef = useRef<HTMLDivElement>(null);
  const leftToggleRef = useRef<HTMLButtonElement>(null);
  const rightToggleRef = useRef<HTMLButtonElement>(null);
  const idBase = useId();
  const leftDrawerId = `${idBase}-left-drawer`;
  const rightDrawerId = `${idBase}-right-drawer`;

  const [openDrawer, setOpenDrawer] = useState<Side | null>(null);
  const openRef = useRef<Side | null>(null);
  const pendingFocusRef = useRef<PendingFocus | null>(null);

  const panelIds = useMemo(() => {
    const ids: string[] = [];
    if (showLeftPanel) ids.push(LEFT_ID);
    ids.push(CENTER_ID);
    if (showRightPanel) ids.push(RIGHT_ID);
    return ids;
  }, [showLeftPanel, showRightPanel]);

  const { defaultLayout, onLayoutChanged } = useWorkspaceLayout({
    workspace,
    key: 'rails',
    panelIds,
  });

  const applyOpen = useCallback(
    (
      next: Side | null,
      options: { focus?: 'into' | HTMLElement; returnFocus?: boolean } = {},
    ) => {
      const current = openRef.current;
      if (current === next) return;
      const { focus, returnFocus = true } = options;
      // The drawer container itself takes focus when its rail has nothing
      // focusable, so the check covers the drawer as well as the host.
      const drawer = (current === 'left' ? leftDrawerRef : rightDrawerRef).current;
      const host = current === 'left' ? leftHost : rightHost;
      if (current && returnFocus && ((drawer && containsFocus(drawer)) || containsFocus(host))) {
        pendingFocusRef.current = { side: current, target: 'toggle' };
      }
      if (next && focus) pendingFocusRef.current = { side: next, target: focus };
      openRef.current = next;
      setOpenDrawer(next);
    },
    [leftHost, rightHost],
  );

  const togglePanel = useCallback((ref: PanelHandle) => {
    const panel = ref.current;
    if (!panel) return;
    if (panel.isCollapsed()) panel.expand();
    else panel.collapse();
  }, []);

  const toggleRail = useCallback(
    (side: Side) => {
      const docked = side === 'left' ? leftDocked : rightDocked;
      if (docked) togglePanel(side === 'left' ? leftPanelRef : rightPanelRef);
      else applyOpen(openRef.current === side ? null : side, { focus: 'into' });
    },
    [leftDocked, rightDocked, togglePanel, leftPanelRef, rightPanelRef, applyOpen],
  );

  // Mode changes and selection keys drive the drawers. Previous values live
  // in a ref so an unrelated render never reopens a drawer the user closed.
  const prevModeRef = useRef({ leftDocked: true, rightDocked: true, leftKey, rightKey });
  useLayoutEffect(() => {
    const prev = prevModeRef.current;
    prevModeRef.current = { leftDocked, rightDocked, leftKey, rightKey };
    const current = openRef.current;
    let next = current;
    let returnFocus = true;
    let focus: HTMLElement | undefined;

    if (
      (current === 'left' && (!hasLeft || leftDocked)) ||
      (current === 'right' && (!hasRight || rightDocked))
    ) {
      next = null;
      returnFocus = false;
    }

    const leftDrawer = hasLeft && !leftDocked;
    const rightDrawer = hasRight && !rightDocked;

    if (leftDrawer && !prev.leftDocked && leftKey !== prev.leftKey && next === 'left') next = null;

    if (rightDrawer) {
      if (prev.rightDocked) {
        if (containsFocus(rightHost) || rightFocusRef.current) {
          next = 'right';
          focus = rightFocusRef.current ?? undefined;
        } else if (rightKey != null) {
          next = 'right';
        }
      } else if (rightKey !== prev.rightKey && rightKey !== undefined) {
        if (rightKey !== null) next = 'right';
        else if (next === 'right') next = null;
      }
    }

    if (leftDrawer && prev.leftDocked && (containsFocus(leftHost) || leftFocusRef.current)) {
      next = 'left';
      focus = leftFocusRef.current ?? undefined;
    }

    applyOpen(next, { returnFocus, focus });
  }, [hasLeft, hasRight, leftDocked, rightDocked, leftKey, rightKey, leftHost, rightHost, applyOpen]);

  // Focus records are only meaningful within the commit that moved a host.
  useLayoutEffect(() => {
    leftFocusRef.current = null;
    rightFocusRef.current = null;
  });

  useLayoutEffect(() => {
    const pending = pendingFocusRef.current;
    if (!pending) return;
    pendingFocusRef.current = null;
    if (pending.target === 'toggle') {
      (pending.side === 'left' ? leftToggleRef : rightToggleRef).current?.focus();
      return;
    }
    const drawer = (pending.side === 'left' ? leftDrawerRef : rightDrawerRef).current;
    if (!drawer || drawer.hidden) return;
    if (pending.target instanceof HTMLElement && drawer.contains(pending.target)) {
      pending.target.focus({ preventScroll: true });
      return;
    }
    (drawer.querySelector<HTMLElement>(FOCUSABLE_SELECTOR) ?? drawer).focus();
  }, [openDrawer]);

  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if (e.key !== '[' && e.key !== ']') return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isTextInputTarget(e.target)) return;
      if (isModalDialogOpen()) return;
      if (e.key === '[' && hasLeft) {
        e.preventDefault();
        toggleRail('left');
      }
      if (e.key === ']' && hasRight) {
        e.preventDefault();
        toggleRail('right');
      }
    }
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [hasLeft, hasRight, toggleRail]);

  // Escape from outside the rail. Presses inside it are handled by the rail
  // wrapper's React handler, which runs after nested handlers.
  useEffect(() => {
    if (!openDrawer) return;
    const host = openDrawer === 'left' ? leftHost : rightHost;
    function handler(e: KeyboardEvent) {
      if (e.key !== 'Escape' || e.defaultPrevented) return;
      if (isTextInputTarget(e.target) || isModalDialogOpen()) return;
      if (e.target instanceof Node && host.contains(e.target)) return;
      applyOpen(null);
    }
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [openDrawer, leftHost, rightHost, applyOpen]);

  useEffect(() => {
    if (openDrawer !== 'left') return;
    function handler(e: MouseEvent) {
      const target = e.target;
      if (!(target instanceof Node)) return;
      if (leftDrawerRef.current?.contains(target)) return;
      if (leftToggleRef.current?.contains(target)) return;
      applyOpen(null, { returnFocus: false });
    }
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [openDrawer, applyOpen]);

  const onRailKeyDown = (side: Side) => (e: React.KeyboardEvent) => {
    if (e.key !== 'Escape' || openDrawer !== side) return;
    if (e.defaultPrevented || isTextInputTarget(e.target) || isModalDialogOpen()) return;
    // The left drawer owns this press; a workspace selection stays put.
    if (side === 'left') e.stopPropagation();
    applyOpen(null);
  };

  const drawerClasses = 'absolute inset-y-0 z-header overflow-hidden bg-surface border-border';

  // A docked toggle lives in the center pane, clear of the separator's hit
  // region. An undocked toggle lives at the shell root so the center pane's
  // overflow never clips it at an open drawer's inner edge.
  const leftToggle = (
    <RailToggle
      side="left"
      expanded={leftDocked ? !leftCollapsed : openDrawer === 'left'}
      controls={leftDocked ? undefined : leftDrawerId}
      offsetPx={leftDocked ? DOCKED_TOGGLE_INSET_PX : openDrawer === 'left' ? leftDrawerWidth : 0}
      inset={leftDocked}
      onToggle={() => toggleRail('left')}
      buttonRef={leftToggleRef}
    />
  );
  const rightToggle = (
    <RailToggle
      side="right"
      expanded={rightDocked ? !rightCollapsed : openDrawer === 'right'}
      controls={rightDocked ? undefined : rightDrawerId}
      offsetPx={rightDocked ? DOCKED_TOGGLE_INSET_PX : openDrawer === 'right' ? rightDrawerWidth : 0}
      inset={rightDocked}
      onToggle={() => toggleRail('right')}
      buttonRef={rightToggleRef}
    />
  );

  return (
    <div ref={rootRef} className={cn('relative flex flex-1 min-h-0 w-full h-full', className)}>
      <Group
        orientation="horizontal"
        className="flex-1 min-h-0 w-full"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
        resizeTargetMinimumSize={RESIZE_TARGET}
      >
        {showLeftPanel && (
          <Panel
            id={LEFT_ID}
            // Numbers are pixels. Percent defaults must be strings, or v4 reads them as pixels.
            defaultSize={`${defaultLeftPct}`}
            minSize={minLeftPx}
            collapsible
            collapsedSize={0}
            panelRef={leftPanelRef}
            onResize={() => setLeftCollapsed(leftPanelRef.current?.isCollapsed() ?? false)}
          >
            <div className="h-full overflow-hidden">
              <RailSlot host={leftHost} focusRef={leftFocusRef} />
            </div>
          </Panel>
        )}

        {showLeftPanel && (
          <Separator id="sep-left" className="group/separator relative w-1.5 select-none focus-visible:-outline-offset-2">
            <SeparatorBody orientation="vertical" />
          </Separator>
        )}

        <Panel
          id={CENTER_ID}
          defaultSize={`${100 - (showLeftPanel ? defaultLeftPct : 0) - (showRightPanel ? defaultRightPct : 0)}`}
          minSize="20%"
        >
          <div className="relative h-full overflow-hidden">
            {children}
            {hasLeft && leftDocked && leftToggle}
            {hasRight && rightDocked && rightToggle}
          </div>
        </Panel>

        {showRightPanel && (
          <Separator id="sep-right" className="group/separator relative w-1.5 select-none focus-visible:-outline-offset-2">
            <SeparatorBody orientation="vertical" />
          </Separator>
        )}

        {showRightPanel && (
          <Panel
            id={RIGHT_ID}
            defaultSize={`${defaultRightPct}`}
            minSize={minRightPx}
            collapsible
            collapsedSize={0}
            panelRef={rightPanelRef}
            onResize={() => setRightCollapsed(rightPanelRef.current?.isCollapsed() ?? false)}
          >
            {/* Elevated detail pane: the leftward edge shadow lets it read as
                lifted above the recessed list. Lives on the wrapper so the cast
                shadow is not clipped by the pane's own internal overflow. */}
            <div className="h-full overflow-hidden shadow-pane-left">
              <RailSlot host={rightHost} focusRef={rightFocusRef} />
            </div>
          </Panel>
        )}
      </Group>

      {/* Drawers carry no lingering transform (the animations have no fill
          mode), so fixed dialogs inside a rail still cover the viewport. */}
      {hasLeft && !leftDocked && (
        <div
          id={leftDrawerId}
          ref={leftDrawerRef}
          hidden={openDrawer !== 'left'}
          tabIndex={-1}
          style={{ width: leftDrawerWidth }}
          className={cn(drawerClasses, 'left-0 border-r shadow-lg animate-drawer-in-left')}
        >
          <RailSlot host={leftHost} focusRef={leftFocusRef} />
        </div>
      )}
      {hasRight && !rightDocked && (
        <div
          id={rightDrawerId}
          ref={rightDrawerRef}
          hidden={openDrawer !== 'right'}
          tabIndex={-1}
          style={{ width: rightDrawerWidth }}
          className={cn(drawerClasses, 'right-0 border-l shadow-pane-left animate-drawer-in-right')}
        >
          <RailSlot host={rightHost} focusRef={rightFocusRef} />
        </div>
      )}
      {hasLeft && !leftDocked && leftToggle}
      {hasRight && !rightDocked && rightToggle}

      {/* Portals come after every slot so a slot adopts its host before the
          rail content commits (autoFocus and layout measurements need it). */}
      {hasLeft && createPortal(
        <div className="h-full" onKeyDown={onRailKeyDown('left')}>{left}</div>,
        leftHost,
      )}
      {hasRight && createPortal(
        <div className="h-full" onKeyDown={onRailKeyDown('right')}>{right}</div>,
        rightHost,
      )}
    </div>
  );
}
