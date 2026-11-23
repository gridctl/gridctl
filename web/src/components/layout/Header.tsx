import { RefreshCw, RotateCcw, Plus, Command, LogOut, SlidersHorizontal } from 'lucide-react';
import { Suspense, lazy, useState, useCallback, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { triggerReload, fetchStackPlan, fetchStackHealth } from '../../lib/api';
import { describeError, isCancellation } from '../../lib/describeError';
import { showToast } from '../ui/Toast';
import { useAuthStore } from '../../stores/useAuthStore';
import { SpecDiffModal } from '../spec/SpecDiffModal';
import { HeaderTelemetryPill } from '../telemetry/HeaderTelemetryPill';
import { useSpecStore } from '../../stores/useSpecStore';
import { useWizardStore } from '../../stores/useWizardStore';
import { LogoWordmark } from './LogoWordmark';
import type { PlanDiff } from '../../types';
import { ThemePicker } from '../shell/ThemePicker';
import { WorkspaceSwitcher } from '../shell/WorkspaceSwitcher';

const CreationWizard = lazy(() => import('../wizard/CreationWizard').then((m) => ({ default: m.CreationWizard })));
const GlobalContextDialog = lazy(() => import('../context/GlobalContextDialog').then((m) => ({ default: m.GlobalContextDialog })));

interface HeaderProps {
  onRefresh?: () => void;
  isRefreshing?: boolean;
}

export function Header({ onRefresh, isRefreshing }: HeaderProps) {
  const navigate = useNavigate();
  const connectionStatus = useStackStore((s) => s.connectionStatus);

  const toggleCommandPalette = useUIStore((s) => s.toggleCommandPalette);
  const credential = useAuthStore((s) => s.credential);
  const [isReloading, setIsReloading] = useState(false);
  // Global Context dialog, reachable from the creation wizard's tile.
  const [showGlobalContext, setShowGlobalContext] = useState(false);
  const wizardOpen = useWizardStore((s) => s.isOpen);
  // Synchronous re-entry guard for the whole reload flow: pre-flight, the
  // preview modal, and the reload. State updates are not visible to a second
  // click in the same tick; isReloading drives the disabled spin.
  const reloadGuard = useRef(false);
  const reloadFlow = useRef(0);

  const releaseReload = useCallback(() => {
    reloadGuard.current = false;
    setIsReloading(false);
  }, []);

  // Best-effort health refresh so Spec mode agrees with the preview. The
  // result is written only while the flow that asked for it is current.
  const refreshHealth = useCallback((flow: number, isCurrent: () => boolean = () => true) => {
    fetchStackHealth().then(
      (health) => {
        if (reloadFlow.current === flow && isCurrent()) useSpecStore.getState().setHealth(health);
      },
      () => undefined,
    );
  }, []);

  const executeReload = useCallback(async () => {
    reloadGuard.current = true;
    setIsReloading(true);
    const flow = reloadFlow.current;

    try {
      const result = await triggerReload();
      showToast(result.success ? 'success' : 'error', result.message);
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Reload');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      refreshHealth(flow);
      releaseReload();
    }
  }, [refreshHealth, releaseReload]);

  const handleReload = useCallback(async () => {
    if (reloadGuard.current) return;
    reloadGuard.current = true;
    setIsReloading(true);
    const flow = ++reloadFlow.current;

    let plan: PlanDiff;
    try {
      plan = await fetchStackPlan();
    } catch (err) {
      if (isCancellation(err)) {
        releaseReload();
        return;
      }
      // The guard stays held until the modal is dismissed or a reload runs.
      useSpecStore.getState().openApplyError(err);
      return;
    }

    // An empty plan does not mean a reload is a no-op: reload also applies
    // blocks the plan does not cover and rotated secrets.
    if (!plan.hasChanges) {
      await executeReload();
      return;
    }

    useSpecStore.getState().openApplyPlan(plan);
    refreshHealth(flow, () => {
      const { diffModalOpen, diffModalMode } = useSpecStore.getState();
      return diffModalOpen && diffModalMode === 'apply';
    });
  }, [executeReload, refreshHealth, releaseReload]);

  // Connection state and server health are surfaced continuously in the global
  // StatusBar; the header no longer mirrors them. We keep `isConnected` only to
  // gate the persistence control, which is meaningless before the gateway is up.
  const isConnected = connectionStatus === 'connected';

  return (
    <header className="h-14 bg-surface/80 backdrop-blur-xl border-b border-border/50 flex items-center justify-between gap-3 px-3 md:gap-4 md:px-5 relative z-header">
      {/* Subtle gradient line at top */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

      {/* Left: Logo + Workspace Switcher. The gateway version is deliberately
          absent here. It is a property of the gateway reported over
          /api/status, not of the UI shell, so it belongs with the gateway's
          other properties on the canvas node and inspector sidebar rather than
          beside the wordmark. */}
      {/* min-w-0 flex-1: this group shrinks so the pill strip can scroll
          instead of pushing the action cluster past the viewport edge. */}
      <div className="flex items-center gap-2 min-w-0 flex-1 md:gap-4">
        {/* Brand Logo — inline so "ctl" follows the theme (readable on light).
            Shorter below sm so one workspace pill stays visible at 390px. */}
        <LogoWordmark className="h-6 w-auto block shrink-0 sm:h-10" />
        <WorkspaceSwitcher />
      </div>

      {/* Right: Persistence quick-toggle + Actions. Persistence is the one
          genuinely actionable control from the old status cluster, so it stays
          in the header next to the other actions. */}
      {/* shrink-0: actions stay reachable; the pill strip absorbs the squeeze.
          Dividers hide below sm so the cluster fits a 390px header. */}
      <div className="flex items-center gap-1 shrink-0 sm:gap-2">
        {isConnected && <HeaderTelemetryPill />}
        {isConnected && <div className="hidden sm:block w-px h-5 bg-border/50 mx-0.5" />}
        {/* Appearance (light/dark/system) — prominent in the action cluster */}
        <ThemePicker />
        <IconButton
          icon={SlidersHorizontal}
          onClick={() => useUIStore.getState().setPreferencesOpen(true)}
          tooltip="Preferences"
          className="hover:text-primary hover:border-primary/30"
        />
        <div className="hidden sm:block w-px h-5 bg-border/50 mx-0.5" />
        {credential && (
          <IconButton
            icon={LogOut}
            onClick={() => useAuthStore.getState().forgetCredential()}
            tooltip="Forget credential"
            className="hover:text-primary hover:border-primary/30"
          />
        )}
        <IconButton
          icon={Command}
          onClick={toggleCommandPalette}
          tooltip="Command Palette (⌘K)"
          className="hover:text-primary hover:border-primary/30"
        />
        <IconButton
          icon={Plus}
          onClick={() => useWizardStore.getState().open()}
          tooltip="Create new"
          className="hover:text-primary hover:border-primary/30"
        />
        <IconButton
          icon={RefreshCw}
          onClick={onRefresh}
          disabled={isRefreshing}
          className={cn(
            isRefreshing && 'animate-spin',
            'hover:text-primary hover:border-primary/30'
          )}
          tooltip="Refresh (⌘⇧R)"
        />
        <IconButton
          icon={RotateCcw}
          onClick={handleReload}
          disabled={isReloading}
          className={cn(
            isReloading && 'animate-spin',
            'hover:text-secondary hover:border-secondary/30'
          )}
          tooltip="Reload Config"
        />
      </div>
      <SpecDiffModal onApply={executeReload} onDismiss={releaseReload} />
      {wizardOpen && (
        <Suspense fallback={null}>
          <CreationWizard
            onOpenVault={() => navigate('/vault')}
            onOpenGlobalContext={() => setShowGlobalContext(true)}
            onOpenConnections={() => navigate('/connections?spotlight=unlinked')}
            onDeploy={onRefresh}
          />
        </Suspense>
      )}
      {/* Portaled: the header's backdrop-blur creates a containing block
          that would trap the dialog's fixed positioning inside the 56px
          header (same reason CreationWizard portals). */}
      {showGlobalContext &&
        createPortal(
          <Suspense fallback={null}>
            <GlobalContextDialog
              isOpen={showGlobalContext}
              onClose={() => setShowGlobalContext(false)}
            />
          </Suspense>,
          document.body,
        )}
    </header>
  );
}
