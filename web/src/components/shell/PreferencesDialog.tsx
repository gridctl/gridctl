import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';
import { Switch } from '../ui/Switch';
import { useTextZoom } from '../../hooks/useTextZoom';
import { TEXT_ZOOM_ROWS, type TextZoomSpec } from '../../lib/textZoomDefaults';
import { COMPACT_MODE_WORKSPACES, useUIStore } from '../../stores/useUIStore';
import { WORKSPACE_CONFIG } from '../../types/workspace';
import { ThemeModeRadioGroup } from './ThemeModeRadioGroup';

const WORKSPACE_LABEL = new Map(WORKSPACE_CONFIG.map((workspace) => [workspace.id, workspace.label]));

function TextSizeRow({ spec }: { spec: TextZoomSpec }) {
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: spec.name,
    defaultSize: spec.defaultSize,
    minSize: spec.minSize,
    maxSize: spec.maxSize,
    step: spec.step,
  });

  return (
    <div className="flex items-center justify-between gap-3 py-1.5">
      <span className="text-xs text-text-secondary">{spec.label}</span>
      <div className="flex items-center gap-1">
        <span className="w-10 text-right font-mono text-xs text-text-primary">{fontSize}px</span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={zoomOut}
          disabled={isMin}
          aria-label={`Decrease ${spec.label} text size`}
        >
          −
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={zoomIn}
          disabled={isMax}
          aria-label={`Increase ${spec.label} text size`}
        >
          +
        </Button>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={resetZoom}
          disabled={isDefault}
          aria-label={`Reset ${spec.label} text size`}
        >
          Reset
        </Button>
      </div>
    </div>
  );
}

export function PreferencesDialog() {
  const open = useUIStore((s) => s.preferencesOpen);
  const setOpen = useUIStore((s) => s.setPreferencesOpen);
  const setShortcutsOpen = useUIStore((s) => s.setShortcutsOpen);
  const compactMode = useUIStore((s) => s.compactMode);
  const setCompactMode = useUIStore((s) => s.setCompactMode);

  return (
    <Modal isOpen={open} onClose={() => setOpen(false)} title="Preferences">
      <div className="space-y-6">
        <section className="space-y-2">
          <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">Appearance</h3>
          <ThemeModeRadioGroup />
        </section>

        <section className="space-y-2">
          <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">Compact mode</h3>
          <p className="text-2xs text-text-muted">
            Cmd+Backslash toggles compact mode for the current workspace.
          </p>
          <ul className="space-y-1">
            {COMPACT_MODE_WORKSPACES.map((id) => {
              const label = WORKSPACE_LABEL.get(id) ?? id;
              return (
                <li key={id} className="flex items-center justify-between gap-3 py-1">
                  <span className="text-xs text-text-secondary">{label}</span>
                  <Switch
                    checked={compactMode[id]}
                    onChange={(next) => setCompactMode(id, next)}
                    label={label}
                  />
                </li>
              );
            })}
          </ul>
        </section>

        <section className="space-y-1">
          <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">Text size</h3>
          {TEXT_ZOOM_ROWS.map((spec) => (
            <TextSizeRow key={spec.name} spec={spec} />
          ))}
        </section>

        <button
          type="button"
          className="text-xs text-primary hover:underline"
          onClick={() => {
            setOpen(false);
            setShortcutsOpen(true);
          }}
        >
          Keyboard shortcuts
        </button>
      </div>
    </Modal>
  );
}
