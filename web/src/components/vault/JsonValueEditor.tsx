import { useMemo } from 'react';
import CodeMirror from '@uiw/react-codemirror';
import { json, jsonParseLinter } from '@codemirror/lang-json';
import { linter, lintGutter } from '@codemirror/lint';
import { EditorView, keymap } from '@codemirror/view';
import { Prec } from '@codemirror/state';

// An empty editor isn't "invalid JSON" yet — skip linting whitespace-only
// content so a fresh field doesn't show a red gutter marker.
const parseLinter = jsonParseLinter();
const jsonLintSource = (view: EditorView) =>
  view.state.doc.toString().trim() === '' ? [] : parseLinter(view);

// Dark theme tuned to the app palette (see web/src/index.css tokens). Kept
// minimal — transparent surface, monospace, no chrome — so the editor reads as
// an inline field rather than a full IDE.
const editorTheme = EditorView.theme(
  {
    '&': { backgroundColor: 'transparent', fontSize: '12px' },
    '&.cm-focused': { outline: 'none' },
    '.cm-content': {
      fontFamily:
        'ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", monospace',
      caretColor: '#fafaf9',
    },
    '.cm-gutters': {
      backgroundColor: 'transparent',
      border: 'none',
      color: 'rgba(120,113,108,0.5)',
    },
    '.cm-activeLine': { backgroundColor: 'rgba(255,255,255,0.02)' },
    '.cm-activeLineGutter': { backgroundColor: 'transparent' },
  },
  { dark: true },
);

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
