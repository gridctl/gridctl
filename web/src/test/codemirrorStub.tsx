// Test stub for @uiw/react-codemirror. CodeMirror needs layout/measurement
// APIs jsdom doesn't implement, so under test we render a plain textarea that
// honors the same value/onChange contract. Aliased in vitest.config.ts.
import type { ChangeEvent } from 'react';

export default function CodeMirrorStub({
  value = '',
  onChange,
}: {
  value?: string;
  onChange?: (value: string) => void;
}) {
  return (
    <textarea
      aria-label="JSON value"
      value={value}
      onChange={(e: ChangeEvent<HTMLTextAreaElement>) =>
        onChange?.(e.target.value)
      }
    />
  );
}
