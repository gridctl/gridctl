import { useMemo } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { json, jsonParseLinter } from '@codemirror/lang-json';
import { linter, lintGutter } from '@codemirror/lint';
import { EditorView, keymap } from '@codemirror/view';
import { Prec } from '@codemirror/state';
import { useResolvedTheme } from '../../themes/useTheme';
import { buildEditorTheme } from '../../themes/codemirror';

// An empty editor isn't "invalid JSON" yet — skip linting whitespace-only
// content so a fresh field doesn't show a red gutter marker.
const parseLinter = jsonParseLinter();
const jsonLintSource = (view: EditorView) =>
  view.state.doc.toString().trim() === '' ? [] : parseLinter(view);

// JsonValueEditor wraps CodeMirror 6 for editing `json` variable values. It
// provides syntax highlighting and an inline lint gutter (parse errors marked
// on the offending line) while leaving save-gating validation to the parent.
// Enter inserts a newline; Cmd/Ctrl+Enter submits and Escape cancels so the
// editor coexists with the surrounding add/edit forms.
export default function JsonValueEditor({
  value,
  onChange,
  onBlur,
  onSubmit,
  onCancel,
  compact,
  ariaLabel = 'JSON value',
}: {
  value: string;
  onChange: (next: string) => void;
  onBlur?: () => void;
  onSubmit?: () => void;
  onCancel?: () => void;
  compact?: boolean;
  ariaLabel?: string;
}) {
  // Rebuild the editor chrome theme on theme change (CodeMirror's `dark` flag
  // and a few colors aren't CSS-var driven).
  const resolved = useResolvedTheme();
  const editorTheme = useMemo(() => buildEditorTheme(resolved), [resolved]);

  const extensions = useMemo(
    () => [
      json(),
      linter(jsonLintSource),
      lintGutter(),
      EditorView.lineWrapping,
      EditorView.domEventHandlers({
        blur: () => {
          onBlur?.();
          return false;
        },
      }),
      // Highest precedence so these win over the default Enter/Escape bindings.
      Prec.highest(
        keymap.of([
          {
            key: 'Mod-Enter',
            run: () => {
              onSubmit?.();
              return true;
            },
          },
          {
            key: 'Escape',
            run: () => {
              onCancel?.();
              return true;
            },
          },
        ]),
      ),
    ],
    [onBlur, onSubmit, onCancel],
  );

  return (
    <CodeMirror
      value={value}
      onChange={onChange}
      extensions={extensions}
      theme={editorTheme}
      basicSetup={{
        lineNumbers: true,
        foldGutter: false,
        highlightActiveLine: false,
        autocompletion: false,
        searchKeymap: false,
      }}
      minHeight={compact ? '60px' : '96px'}
      maxHeight={compact ? '160px' : '220px'}
      aria-label={ariaLabel}
      className="rounded-lg border border-border overflow-hidden bg-surface"
    />
  );
}
