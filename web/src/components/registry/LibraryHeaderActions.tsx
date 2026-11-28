import { useCallback, useRef } from 'react';
import {
  AppWindow,
  CloudDownload,
  Download,
  Ellipsis,
  ExternalLink,
  Plus,
  RefreshCw,
} from 'lucide-react';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { HeaderMenu, type HeaderMenuItem } from '../shell/HeaderMenu';

interface LibraryHeaderActionsProps {
  onNewSkill: () => void;
  onImportSkill: () => void;
  onRefresh: () => void;
  onSync: () => void;
  onPopout: () => void;
  hasSources: boolean;
  updateCount: number;
  syncing: boolean;
  detached: boolean;
  /** Moves every action but New Skill and the update pill into a "More actions" menu. */
  collapsed: boolean;
}

const ACTION_CLASS =
  'flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium whitespace-nowrap text-secondary hover:text-secondary/80 border border-secondary/25 hover:bg-secondary/10 rounded-lg transition-colors';

/**
 * The Library header's action cluster. When the header row is too narrow for
 * the full set (`collapsed`), the secondary actions move into a menu; New Skill
 * and a pending-update Sync pill stay inline.
 */
export function LibraryHeaderActions({
  onNewSkill,
  onImportSkill,
  onRefresh,
  onSync,
  onPopout,
  hasSources,
  updateCount,
  syncing,
  detached,
  collapsed,
}: LibraryHeaderActionsProps) {
  const newSkillRef = useRef<HTMLButtonElement>(null);

  // The menu unmounts when the row widens past the threshold. A ref cleanup
  // runs before its node leaves the DOM, so focus held by the trigger or an
  // item moves to New Skill instead of falling to the body.
  const menuWrapperRef = useCallback((node: HTMLDivElement | null) => {
    if (!node) return;
    return () => {
      if (node.contains(document.activeElement)) newSkillRef.current?.focus();
    };
  }, []);

  const newSkill = (
    <button ref={newSkillRef} onClick={onNewSkill} className={ACTION_CLASS}>
      <Plus size={12} /> New Skill
    </button>
  );

  if (collapsed) {
    const items: HeaderMenuItem[] = [
      { key: 'import', label: 'Import', icon: Download, onSelect: onImportSkill },
    ];
    if (hasSources && updateCount === 0) {
      items.push({
        key: 'sync',
        label: syncing ? 'Syncing…' : 'Sync sources',
        icon: CloudDownload,
        onSelect: onSync,
        disabled: syncing,
      });
    }
    items.push(
      { key: 'refresh', label: 'Refresh', icon: RefreshCw, onSelect: onRefresh },
      detached
        ? { key: 'popout', label: 'Focus detached window', icon: AppWindow, onSelect: onPopout }
        : { key: 'popout', label: 'Open in separate window', icon: ExternalLink, onSelect: onPopout },
    );
    return (
      <>
        {newSkill}
        {updateCount > 0 && (
          <SyncSourcesButton hasSources={hasSources} updateCount={updateCount} syncing={syncing} onClick={onSync} />
        )}
        <div ref={menuWrapperRef} className="flex items-center">
          <HeaderMenu
            tooltip="More actions"
            menuLabel="More actions"
            icon={Ellipsis}
            size="sm"
            variant="ghost"
            items={items}
          />
        </div>
      </>
    );
  }

  return (
    <>
      {/* One shared treatment for the two header actions (the
          secondary outline Import always had): uniform color, padding,
          and nowrap so no label ever two-lines and heights stay equal.
          The stateful Sync pill keeps its distinct alert styling. */}
      {newSkill}
      {/* Adding a git source belongs where sources are managed. Opens the
          same import wizard the registry sidebar does, skipping the
          generic resource-type step. */}
      <button
        onClick={onImportSkill}
        className={ACTION_CLASS}
        title="Import skills from a git repository"
      >
        <Download size={12} /> Import
      </button>
      <SyncSourcesButton
        hasSources={hasSources}
        updateCount={updateCount}
        syncing={syncing}
        onClick={onSync}
      />
      <IconButton icon={RefreshCw} onClick={onRefresh} tooltip="Refresh" size="sm" variant="ghost" />
      <PopoutButton onClick={onPopout} detached={detached} />
    </>
  );
}

interface SyncSourcesButtonProps {
  hasSources: boolean;
  updateCount: number;
  syncing: boolean;
  onClick: () => void;
}

/**
 * Workspace-level "Sync sources" affordance. Three states:
 * - hidden when no imported sources exist (feature is dormant)
 * - brand pill "Sync sources (N updates)" when any source has updateAvailable
 * - low-emphasis IconButton otherwise (lets users force a sync anyway)
 *
 * During sync the button disables and the label morphs inside an aria-live
 * region so screen readers announce the in-flight state. `CloudDownload`
 * differentiates the sync action from the local Refresh button (which uses
 * `RefreshCw`, matching the global Header convention).
 */
function SyncSourcesButton({ hasSources, updateCount, syncing, onClick }: SyncSourcesButtonProps) {
  if (!hasSources) return null;

  const hasUpdates = updateCount > 0;
  const ariaLabel = syncing
    ? 'Syncing skill sources'
    : hasUpdates
      ? `Sync sources, ${updateCount} ${updateCount === 1 ? 'update' : 'updates'} available`
      : 'Sync sources from git';

  if (hasUpdates) {
    // Pill mirrors the per-source pill in SourceGroupHeader so the two
    // affordances read as the same family. Brand tokens rather than the warning
    // family: an available update is informational and optional, matching the
    // "N updates" pill in RegistrySidebar.
    return (
      <button
        type="button"
        onClick={onClick}
        disabled={syncing}
        aria-label={ariaLabel}
        aria-busy={syncing}
        title={ariaLabel}
        className="inline-flex items-center gap-1.5 text-xs px-3 py-1.5 whitespace-nowrap rounded-lg border border-primary/30 bg-primary/10 text-primary hover:bg-primary/20 transition-colors disabled:opacity-60"
      >
        <CloudDownload size={12} aria-hidden="true" className={syncing ? 'animate-pulse' : undefined} />
        <span aria-live="polite">
          {syncing ? 'Syncing…' : `Sync sources (${updateCount} update${updateCount === 1 ? '' : 's'})`}
        </span>
      </button>
    );
  }

  // No pending updates: a quiet IconButton lets users force a sync without
  // the chrome implying anything is wrong. The aria-label morph carries the
  // in-flight announcement; no separate live region is needed.
  return (
    <IconButton
      icon={CloudDownload}
      onClick={onClick}
      disabled={syncing}
      tooltip={ariaLabel}
      size="sm"
      variant="ghost"
      className={syncing ? 'animate-pulse' : undefined}
    />
  );
}
