import { useId, type ReactNode } from 'react';
import { RefreshCw } from 'lucide-react';
import { WORKSPACE_LABELS } from '../../types/workspace';
import { useUIStore } from '../../stores/useUIStore';
import type { RegistryKind } from '../../lib/registryKind';
import { IconButton } from '../ui/IconButton';
import { WorkspaceHeader } from '../ui/WorkspaceHeader';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { LibraryKindSwitch } from '../registry/LibraryKindSwitch';

interface ContextFrameProps {
  onKindChange: (kind: RegistryKind) => void;
  onRefresh: () => void;
  inspector?: ReactNode;
  inspectorKey?: string | null;
  children: ReactNode;
}

/**
 * Shared Library segment shell for every state of Global Context (loading,
 * failed, setup, single-file, and fragments). Uses WorkspaceHeader for the
 * single visible h1 and kind switch, and mounts the right-rail inspector
 * only when supplied.
 */
export function ContextFrame({
  onKindChange,
  onRefresh,
  inspector,
  inspectorKey,
  children,
}: ContextFrameProps) {
  const headingId = useId();
  const compact = useUIStore((st) => st.compactMode.library);

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="library"
        defaultRightPct={30}
        minRightPx={320}
        right={inspector}
        rightKey={inspectorKey}
      >
        <section aria-labelledby={headingId} className="flex flex-col h-full overflow-hidden">
          <WorkspaceHeader
            title={WORKSPACE_LABELS.library}
            headingId={headingId}
            compact={compact}
            switcher={<LibraryKindSwitch kind="context" onChange={onKindChange} />}
            actions={
              <IconButton
                icon={RefreshCw}
                onClick={onRefresh}
                tooltip="Refresh"
                size="sm"
                variant="ghost"
              />
            }
          />
          <div className="flex-1 min-h-0 flex flex-col overflow-hidden">{children}</div>
        </section>
      </WorkspaceShell>
    </div>
  );
}
