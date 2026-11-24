import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { Maximize2, Minimize2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { detachedDocumentTitle } from '../../lib/detachedWindows';
import { useDetachedWindowSync, type DetachableWindow } from '../../hooks/useBroadcastChannel';
import { useAuthStore } from '../../stores/useAuthStore';
import { IconButton } from '../ui/IconButton';
import { FailureBanner } from '../ui/EmptyState';
import { ToastContainer, showToast } from '../ui/Toast';

const GRAIN_IMAGE = `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`;

const DEFAULT_DISCONNECTED_MESSAGE = 'Gateway unavailable. Showing the last loaded data.';

export interface DetachedWindowConnection {
  lost: boolean;
  message?: string;
  onRetry?: () => void;
}

export interface DetachedWindowHeader {
  /** The page's icon tile. */
  icon: ReactNode;
  /** Title text or menu, and badges. */
  start: ReactNode;
  /** Zoom controls, refresh, and page buttons. The fullscreen toggle follows them. */
  actions?: ReactNode;
  /** Color of the top hairline. */
  accent?: 'primary' | 'tertiary';
}

interface DetachedWindowFrameProps {
  windowType: DetachableWindow;
  /** Appended to the document title, for example the selected node. */
  titleDetail?: string | null;
  /** Omit to render no header bar. */
  header?: DetachedWindowHeader;
  fullscreen?: boolean;
  /** Omit to render no disconnected banner. */
  connection?: DetachedWindowConnection;
  footer?: boolean;
  footerStart?: ReactNode;
  /** The page's main landmark and anything that sits between it and the header. */
  children: ReactNode;
}

async function toggleDocumentFullscreen() {
  try {
    if (document.fullscreenElement) await document.exitFullscreen();
    else await document.documentElement.requestFullscreen();
  } catch {
    showToast('warning', 'Fullscreen is not available in this window');
  }
}

// Shared chrome for every detached page. It renders what it is given and
// fetches nothing: connection state comes from the page, which owns the one
// status poll for its window and knows when its own failure notice already
// covers an outage.
export function DetachedWindowFrame({
  windowType,
  titleDetail,
  header,
  fullscreen = false,
  connection,
  footer = true,
  footerStart,
  children,
}: DetachedWindowFrameProps) {
  const authRequired = useAuthStore((s) => s.authRequired);
  const [isFullscreen, setIsFullscreen] = useState(() => !!document.fullscreenElement);

  useDetachedWindowSync(windowType);

  useEffect(() => {
    document.title = detachedDocumentTitle(windowType, titleDetail);
  }, [windowType, titleDetail]);

  useEffect(() => {
    if (!fullscreen) return;
    const handler = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', handler);
    return () => document.removeEventListener('fullscreenchange', handler);
  }, [fullscreen]);

  const onToggleFullscreen = useCallback(() => {
    void toggleDocumentFullscreen();
  }, []);

  const showBanner = connection?.lost === true && !authRequired;
  const accent = header?.accent ?? 'primary';

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden">
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{ backgroundImage: GRAIN_IMAGE }}
      />

      {header && (
        <header className="h-12 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-b border-border/50 flex items-center justify-between gap-3 px-4 z-raised relative">
          <div
            className={cn(
              'absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent to-transparent',
              accent === 'tertiary' ? 'via-tertiary/30' : 'via-primary/30',
            )}
          />
          <div className="flex items-center gap-3 min-w-0">
            {header.icon}
            {header.start}
          </div>
          {(header.actions || fullscreen) && (
            <div className="flex items-center gap-2">
              {header.actions}
              {fullscreen && (
                <IconButton
                  icon={isFullscreen ? Minimize2 : Maximize2}
                  onClick={onToggleFullscreen}
                  tooltip={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
                  size="sm"
                  variant="ghost"
                />
              )}
            </div>
          )}
        </header>
      )}

      {showBanner && (
        <FailureBanner
          message={connection.message ?? DEFAULT_DISCONNECTED_MESSAGE}
          onRetry={connection.onRetry}
          className="mx-4 mt-2 mb-0 flex-shrink-0"
        />
      )}

      {children}

      {footer && (
        <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-3xs text-text-muted">
          <span className="flex items-center gap-2">{footerStart}</span>
          <span className="flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-text-muted animate-pulse motion-reduce:animate-none" />
            Detached Window
          </span>
        </footer>
      )}

      <ToastContainer />
    </div>
  );
}
