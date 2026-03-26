import { RefreshCw, Settings, Zap, RotateCcw, Plus, Command } from 'lucide-react';
import { useState, useRef, useCallback } from 'react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import { IconButton } from '../ui/IconButton';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { triggerReload, fetchStackSpec, validateStackSpec } from '../../lib/api';
import { VaultPanel } from '../vault/VaultPanel';
import { SpecDiffModal } from '../spec/SpecDiffModal';
import { CreationWizard } from '../wizard/CreationWizard';
import { useSpecStore } from '../../stores/useSpecStore';
import { useWizardStore } from '../../stores/useWizardStore';
import logoSvg from '../../assets/brand/logo.svg';

interface HeaderProps {
  onRefresh?: () => void;
  isRefreshing?: boolean;
}

export function Header({ onRefresh, isRefreshing }: HeaderProps) {
  const gatewayInfo = useStackStore((s) => s.gatewayInfo);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const connectionStatus = useStackStore((s) => s.connectionStatus);

  const showVault = useUIStore((s) => s.showVault);
  const setShowVault = useUIStore((s) => s.setShowVault);
  const toggleCommandPalette = useUIStore((s) => s.toggleCommandPalette);
  const [isReloading, setIsReloading] = useState(false);
  const [reloadMessage, setReloadMessage] = useState<{ text: string; isError: boolean } | null>(null);
  const [validationErrors, setValidationErrors] = useState<string[]>([]);
  const dismissTimer = useRef<ReturnType<typeof setTimeout>>(null);

  const executeReload = useCallback(async () => {
    setIsReloading(true);
    setReloadMessage(null);
    if (dismissTimer.current) clearTimeout(dismissTimer.current);

    try {
      const result = await triggerReload();
      setReloadMessage({ text: result.message, isError: !result.success });
      // On successful reload, promote the pending spec (what the user approved)
      // to the applied baseline. Fall back to current spec for direct reloads.
      if (result.success) {
        const store = useSpecStore.getState();
        const applied = store.pendingSpec
          ? { path: store.spec?.path ?? '', content: store.pendingSpec }
          : store.spec;
        if (applied) {
          store.setAppliedSpec(applied);
        }
      }
    } catch (err) {
      setReloadMessage({
        text: err instanceof Error ? err.message : 'Reload failed',
        isError: true,
      });
    } finally {
      setIsReloading(false);
      dismissTimer.current = setTimeout(() => setReloadMessage(null), 4000);
    }
  }, []);

  const handleReload = useCallback(async () => {
    // Fetch the new spec from disk and diff against the applied baseline
    try {
      const newSpec = await fetchStackSpec();
      const store = useSpecStore.getState();

      // Seed appliedSpec on first use (e.g. user never opened Spec tab)
      if (!store.appliedSpec) {
        store.setAppliedSpec(newSpec);
      }

      const appliedSpec = useSpecStore.getState().appliedSpec;

      // If the disk spec differs from what the gateway last applied, show diff
      if (appliedSpec && newSpec.content !== appliedSpec.content) {
        // Validate the new spec
        const result = await validateStackSpec(newSpec.content);
        const errors = (result.issues ?? [])
          .filter((i) => i.severity === 'error')
          .map((i) => `${i.field}: ${i.message}`);
        setValidationErrors(errors);
        useSpecStore.getState().openDiffModal(newSpec.content);
        return;
      }

    } catch {
      // If spec fetch fails, fall through to direct reload
    }

    executeReload();
  }, [executeReload]);

  const runningCount = (mcpServers ?? []).filter((s) => s.initialized).length;
  const totalCount = (mcpServers ?? []).length;
  const unhealthyCount = (mcpServers ?? []).filter((s) => s.healthy === false).length;
  const isConnected = connectionStatus === 'connected';

  return (
    <header className="h-14 bg-surface/80 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-5 relative z-30">
      {/* Subtle gradient line at top */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

      {/* Left: Logo & Version */}
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-3">
          {/* Brand Logo */}
          <img
            src={logoSvg}
            alt="Gridctl"
            className="h-10 w-auto block"
          />
          {/* Version */}
          {gatewayInfo?.version && (
            <span className="text-xs font-mono text-text-muted tracking-wide">
              {gatewayInfo.version}
            </span>
          )}
        </div>
      </div>

      {/* Center: Status Pills */}
      <div className="flex items-center gap-3">
        {/* Connection Status */}
        <div className={cn(
          'flex items-center gap-2 px-3 py-1.5 rounded-full',
          'bg-surface-elevated/60 backdrop-blur-sm border',
          isConnected ? 'border-status-running/20' : 'border-status-error/20'
        )}>
          <StatusDot status={isConnected ? 'running' : 'error'} />
          <span className="text-xs font-medium text-text-secondary">
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>

        {/* Server Count */}
        {totalCount > 0 && (
          <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-surface-elevated/60 backdrop-blur-sm border border-border/50">
            <Zap size={12} className="text-primary" />
            <span className="text-xs font-medium">
              <span className="text-status-running">{runningCount}</span>
              <span className="text-text-muted mx-0.5">/</span>
              <span className="text-text-secondary">{totalCount}</span>
              <span className="text-text-muted ml-1.5">active</span>
              {unhealthyCount > 0 && (
                <span className="text-status-error ml-1.5">
                  ({unhealthyCount} unhealthy)
                </span>
              )}
            </span>
          </div>
        )}
      </div>

      {/* Reload notification */}
      {reloadMessage && (
        <div className={cn(
          'absolute top-full left-1/2 -translate-x-1/2 mt-2 z-50',
          'px-4 py-2 rounded-lg backdrop-blur-xl border shadow-lg',
          'text-xs font-medium transition-all duration-300 animate-fade-in-scale',
          reloadMessage.isError
            ? 'bg-status-error/10 border-status-error/20 text-status-error'
            : 'bg-status-running/10 border-status-running/20 text-status-running'
        )}>
          {reloadMessage.text}
        </div>
      )}

      {/* Right: Actions */}
      <div className="flex items-center gap-2">
        <IconButton
          icon={Command}
          onClick={toggleCommandPalette}
          tooltip="Command Palette (⌘K)"
          className="hover:text-primary hover:border-primary/30"
        />
        <IconButton
          icon={Plus}
          onClick={() => useWizardStore.getState().open()}
          tooltip="Create Resource"
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
          tooltip="Refresh (⌘R)"
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
        <IconButton
          icon={Settings}
          onClick={() => setShowVault(!showVault)}
          tooltip="Vault"
          className={cn(
            'hover:text-primary hover:border-primary/30',
            showVault && 'text-primary border-primary/30'
          )}
        />
      </div>
      {showVault && <VaultPanel onClose={() => setShowVault(false)} />}
      <SpecDiffModal
        onApply={executeReload}
        validationErrors={validationErrors}
      />
      <CreationWizard onOpenVault={() => setShowVault(true)} />
    </header>
  );
}
