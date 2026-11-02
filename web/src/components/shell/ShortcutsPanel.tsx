import { Modal } from '../ui/Modal';
import { ShortcutKeys } from '../ui/ShortcutKeys';
import { SHORTCUT_BINDINGS, SHORTCUT_SCOPE_ORDER } from '../../lib/shortcuts';
import { useUIStore } from '../../stores/useUIStore';

export function ShortcutsPanel() {
  const open = useUIStore((s) => s.shortcutsOpen);
  const setOpen = useUIStore((s) => s.setShortcutsOpen);
  const setPreferencesOpen = useUIStore((s) => s.setPreferencesOpen);

  const groups = SHORTCUT_SCOPE_ORDER.map((scope) => ({
    scope,
    rows: SHORTCUT_BINDINGS.filter((binding) => binding.scope === scope),
  })).filter((group) => group.rows.length > 0);

  return (
    <Modal isOpen={open} onClose={() => setOpen(false)} title="Keyboard shortcuts" size="wide">
      <div className="space-y-5">
        {groups.map((group) => (
          <section key={group.scope} aria-label={group.scope}>
            <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted mb-2">{group.scope}</h3>
            <ul className="divide-y divide-border/30">
              {group.rows.map((binding) => (
                <li
                  key={`${group.scope}:${binding.keys.join('+')}:${binding.label}`}
                  className="flex items-center justify-between gap-3 py-1.5"
                >
                  <span className="text-xs text-text-secondary">{binding.label}</span>
                  <span className="flex items-center gap-2">
                    <ShortcutKeys keys={binding.keys} />
                    <span className="sr-only">{binding.scope}</span>
                  </span>
                </li>
              ))}
            </ul>
          </section>
        ))}
        <button
          type="button"
          className="text-xs text-primary hover:underline"
          onClick={() => {
            setOpen(false);
            setPreferencesOpen(true);
          }}
        >
          Preferences
        </button>
      </div>
    </Modal>
  );
}
