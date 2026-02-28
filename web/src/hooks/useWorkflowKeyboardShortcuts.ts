import { useEffect, useCallback } from 'react';

interface WorkflowShortcutOptions {
  /** Callback when mode should switch (1=code, 2=visual, 3=test) */
  onModeChange?: (mode: 'code' | 'visual' | 'test') => void;
  /** Toggle follow mode */
  onToggleFollow?: () => void;
  /** Delete selected step or edge */
  onDelete?: () => void;
  /** Deselect / close inspector */
  onEscape?: () => void;
  /** Run workflow (Ctrl/Cmd+Enter) */
  onRun?: () => void;
  /** Validate workflow (Ctrl/Cmd+Shift+V) */
  onValidate?: () => void;
  /** Toggle toolbox palette */
  onToggleToolbox?: () => void;
  /** Toggle inspector panel */
  onToggleInspector?: () => void;
  /** Auto-layout / tidy */
  onAutoLayout?: () => void;
  /** Save (Ctrl/Cmd+S) */
  onSave?: () => void;
  /** Whether shortcuts are active */
  enabled?: boolean;
}

function isInputElement(el: HTMLElement): boolean {
  const tag = el.tagName?.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || el.isContentEditable;
}

export function useWorkflowKeyboardShortcuts(options: WorkflowShortcutOptions) {
  const {
    onModeChange,
    onToggleFollow,
    onDelete,
    onEscape,
    onRun,
    onValidate,
    onToggleToolbox,
    onToggleInspector,
    onAutoLayout,
    onSave,
    enabled = true,
  } = options;

  const handler = useCallback((e: KeyboardEvent) => {
    if (!enabled) return;

    const isMod = e.metaKey || e.ctrlKey;
    const target = e.target as HTMLElement;
    const inInput = isInputElement(target);

    // Mod shortcuts work even in inputs
    if (isMod) {
      // Ctrl/Cmd+Enter: Run workflow
      if (e.key === 'Enter' && onRun) {
        e.preventDefault();
        onRun();
        return;
      }
      // Ctrl/Cmd+Shift+V: Validate
      if (e.shiftKey && (e.key === 'V' || e.key === 'v') && onValidate) {
        e.preventDefault();
        onValidate();
        return;
      }
      // Ctrl/Cmd+S: Save
      if (e.key === 's' && onSave) {
        e.preventDefault();
        onSave();
        return;
      }
    }

    // Don't handle single-key shortcuts when typing
    if (inInput) return;

    // Mode switching
    if (e.key === '1' && onModeChange) { e.preventDefault(); onModeChange('code'); return; }
    if (e.key === '2' && onModeChange) { e.preventDefault(); onModeChange('visual'); return; }
    if (e.key === '3' && onModeChange) { e.preventDefault(); onModeChange('test'); return; }

    // Follow mode
    if (e.key === 'f' && onToggleFollow) { e.preventDefault(); onToggleFollow(); return; }

    // Delete/Backspace: delete selected
    if ((e.key === 'Delete' || e.key === 'Backspace') && onDelete) {
      e.preventDefault();
      onDelete();
      return;
    }

    // Escape: deselect
    if (e.key === 'Escape' && onEscape) { onEscape(); return; }

    // Toggle toolbox
    if (e.key === 't' && onToggleToolbox) { e.preventDefault(); onToggleToolbox(); return; }

    // Toggle inspector
    if (e.key === 'i' && onToggleInspector) { e.preventDefault(); onToggleInspector(); return; }

    // Auto-layout
    if (e.key === 'l' && onAutoLayout) { e.preventDefault(); onAutoLayout(); return; }
  }, [enabled, onModeChange, onToggleFollow, onDelete, onEscape, onRun, onValidate, onToggleToolbox, onToggleInspector, onAutoLayout, onSave]);

  useEffect(() => {
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [handler]);
}
