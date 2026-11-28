import {
  CircleQuestionMark,
  Command,
  Ellipsis,
  ExternalLink,
  FileCog,
  Keyboard,
  Loader2,
  LogOut,
  Plus,
  RefreshCw,
  SlidersHorizontal,
} from 'lucide-react';
import { Suspense, lazy, useState, useCallback, useEffect, useRef } from 'react';
import { useGuardedNavigate } from '../../hooks/useGuardedNavigate';
import { formatShortcut, MOD_KEY, SHIFT_KEY } from '../../lib/platform';
import { Button } from '../ui/Button';
import { IconButton } from '../ui/IconButton';
import { ShortcutKeys } from '../ui/ShortcutKeys';
import { HeaderMenu, type HeaderMenuItem } from '../shell/HeaderMenu';
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
import { useShellActionsStore } from '../../stores/useShellActionsStore';
import { LogoWordmark } from './LogoWordmark';
import type { PlanDiff } from '../../types';
import { ThemePicker } from '../shell/ThemePicker';
import { WorkspaceSwitcher } from '../shell/WorkspaceSwitcher';

const CreationWizard = lazy(() => import('../wizard/CreationWizard').then((m) => ({ default: m.CreationWizard })));

interface HeaderProps {
  onRefresh?: () => void;
  isRefreshing?: boolean;
}

export function Header({ onRefresh, isRefreshing }: HeaderProps) {
  const navigate = useGuardedNavigate();
  const connectionStatus = useStackStore((s) => s.connectionStatus);

  const toggleCommandPalette = useUIStore((s) => s.toggleCommandPalette);
  const credential = useAuthStore((s) => s.credential);
  const [isReloading, setIsReloading] = useState(false);
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

  // The palette's Reload Config runs this same flow. handleReload ignores a
  // call while a reload is in flight.
  useEffect(() => {
    const { register, clear } = useShellActionsStore.getState();
    register({ reloadConfig: () => void handleReload() });
    return clear;
  }, [handleReload]);

  // Connection state and server health are surfaced continuously in the global
  // StatusBar; the header no longer mirrors them. We keep `isConnected` only to
  // gate the persistence control, which is meaningless before the gateway is up.
  const isConnected = connectionStatus === 'connected';

  const moreItems: HeaderMenuItem[] = [
    {
      key: 'refresh',
      label: 'Refresh',
      icon: RefreshCw,
      hint: <ShortcutKeys keys={[MOD_KEY, SHIFT_KEY, 'R']} />,
      onSelect: onRefresh,
      disabled: isRefreshing,
      spinning: isRefreshing,
    },
    {
      key: 'reload',
      label: 'Reload Config',
      icon: FileCog,
      onSelect: () => void handleReload(),
      disabled: isReloading,
      spinning: isReloading,
    },
    ...(credential
      ? [
          {
            key: 'forget-credential',
            label: 'Forget credential…',
            icon: LogOut,
            onSelect: () => useUIStore.getState().setForgetCredentialOpen(true),
            separatorBefore: true,
          },
        ]
      : []),
  ];

  const helpItems: HeaderMenuItem[] = [
    {
      key: 'shortcuts',
      label: 'Keyboard shortcuts',
      icon: Keyboard,
      hint: <ShortcutKeys keys={['?']} />,
      onSelect: () => useUIStore.getState().setShortcutsOpen(true),
    },
    {
      key: 'docs',
      label: 'Documentation',
      icon: ExternalLink,
      href: 'https://github.com/gridctl/gridctl/tree/main/docs',
    },
  ];

  const busy = Boolean(isRefreshing) || isReloading;

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
      {/* @container/header: workspace labels show when this group has room for
          them. A container query, not a viewport breakpoint, because the
          action cluster's width varies with the Persistence label. */}
      <div className="@container/header flex items-center gap-2 min-w-0 flex-1 md:gap-4">
        {/* Brand Logo — inline so "ctl" follows the theme (readable on light).
            Shorter below sm to leave the pill strip room on narrow headers. */}
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
        <IconButton
          icon={Command}
          onClick={toggleCommandPalette}
          tooltip={`Command Palette (${formatShortcut([MOD_KEY, 'K'])})`}
          className="hover:text-primary hover:border-primary/30"
        />
        <HeaderMenu tooltip="Help" menuLabel="Help" icon={CircleQuestionMark} items={helpItems} />
        <Button
          variant="primary"
          size="sm"
          onClick={() => useWizardStore.getState().open()}
          aria-label="Create new"
          title="Create new"
          className="h-[34px] min-w-[34px] px-0 min-[1360px]:px-3 shrink-0 hover:translate-y-0"
        >
          <Plus size={16} aria-hidden="true" />
          <span className="hidden min-[1360px]:inline">Create new</span>
        </Button>
        <HeaderMenu
          tooltip="More actions"
          menuLabel="More actions"
          icon={busy ? Loader2 : Ellipsis}
          spinning={busy}
          items={moreItems}
        />
      </div>
      <SpecDiffModal onApply={executeReload} onDismiss={releaseReload} />
      {wizardOpen && (
        <Suspense fallback={null}>
          <CreationWizard
            onOpenVault={() => navigate('/vault')}
            onOpenGlobalContext={() => navigate('/library?kind=context')}
            onOpenConnections={() => navigate('/connections?spotlight=unlinked')}
            onDeploy={onRefresh}
          />
        </Suspense>
      )}
    </header>
  );
}
