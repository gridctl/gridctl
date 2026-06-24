import { EditorView } from '@codemirror/view';
import type { Extension } from '@codemirror/state';
import { readThemeColors } from './readThemeColors';
import type { ResolvedTheme } from './types';

// Build a CodeMirror theme extension from the live theme colors. CodeMirror's
// theme is a compiled object (not CSS-var driven for its `dark` flag and a few
// chrome colors), so editors rebuild this on theme change. Kept minimal —
// transparent surface, monospace, no chrome — so it reads as an inline field.
export function buildEditorTheme(resolved: ResolvedTheme): Extension {
  const c = readThemeColors();
  return EditorView.theme(
    {
      '&': { backgroundColor: 'transparent', fontSize: '12px' },
      '&.cm-focused': { outline: 'none' },
      '.cm-content': {
        fontFamily:
          'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
        caretColor: c.textPrimary,
        color: c.textSecondary,
      },
      '.cm-gutters': {
        backgroundColor: 'transparent',
        border: 'none',
        color: c.textMuted,
      },
      '.cm-activeLine': {
        backgroundColor:
          resolved === 'light' ? 'rgba(20,16,10,0.03)' : 'rgba(255,255,255,0.02)',
      },
      '.cm-activeLineGutter': { backgroundColor: 'transparent' },
    },
    { dark: resolved === 'dark' },
  );
}
