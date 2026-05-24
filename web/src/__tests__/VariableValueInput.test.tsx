import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VariableValueInput } from '../components/vault/VariableValueInput';
import type { VariableType } from '../lib/api';

// CodeMirror needs layout APIs jsdom lacks; swap it for a plain textarea so we
// can exercise the json path (highlighting/lint are CM's concern, not ours).
vi.mock('@uiw/react-codemirror', () => ({
  default: ({
    value,
    onChange,
  }: {
    value: string;
    onChange: (v: string) => void;
  }) => (
    <textarea
      aria-label="JSON value"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}));

function Harness({
  type,
  isSecret = false,
  initial = '',
}: {
  type: VariableType;
  isSecret?: boolean;
  initial?: string;
}) {
  const [value, setValue] = useState(initial);
  const [revealed, setRevealed] = useState(!isSecret);
  const [valid, setValid] = useState(true);
  return (
    <>
      <VariableValueInput
        type={type}
        value={value}
        onChange={setValue}
        isSecret={isSecret}
        revealed={revealed}
        onToggleReveal={() => setRevealed((r) => !r)}
        onValidityChange={setValid}
      />
      <output data-testid="value">{value}</output>
      <output data-testid="valid">{String(valid)}</output>
    </>
  );
}

const value = () => screen.getByTestId('value').textContent;
const valid = () => screen.getByTestId('valid').textContent;

describe('VariableValueInput — bool', () => {
  it('seeds a concrete "false" and toggles to "true"', async () => {
    render(<Harness type="bool" />);
    const sw = screen.getByRole('switch');
    await waitFor(() => expect(value()).toBe('false'));
    expect(sw).toHaveAttribute('aria-checked', 'false');
    fireEvent.click(sw);
    expect(value()).toBe('true');
    expect(sw).toHaveAttribute('aria-checked', 'true');
  });
});

describe('VariableValueInput — number', () => {
  it('reports validity and steps the value', () => {
    render(<Harness type="number" />);
    const input = screen.getByRole('textbox');
    fireEvent.change(input, { target: { value: 'abc' } });
    expect(valid()).toBe('false');
    fireEvent.change(input, { target: { value: '41' } });
    expect(valid()).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Increment' }));
    expect(value()).toBe('42');
  });

  it('shows an inline error only after blur', () => {
    render(<Harness type="number" />);
    const input = screen.getByRole('textbox');
    fireEvent.change(input, { target: { value: 'abc' } });
    expect(screen.queryByText(/invalid number/)).toBeNull();
    fireEvent.blur(input);
    expect(screen.getByText(/invalid number/)).toBeInTheDocument();
  });
});

describe('VariableValueInput — list', () => {
  it('commits tags on Enter and comma, emitting the comma form', () => {
    render(<Harness type="list" />);
    const input = screen.getByRole('textbox', { name: 'Add list item' });
    fireEvent.change(input, { target: { value: 'a' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(value()).toBe('a');
    fireEvent.change(input, { target: { value: 'b,' } });
    expect(value()).toBe('a, b');
  });

  it('rejects duplicates and removes the last chip on Backspace', () => {
    render(<Harness type="list" initial="a, b" />);
    const input = screen.getByRole('textbox', { name: 'Add list item' });
    fireEvent.change(input, { target: { value: 'a' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(value()).toBe('a, b'); // duplicate ignored
    fireEvent.keyDown(input, { key: 'Backspace' });
    expect(value()).toBe('a');
  });

  it('removes a specific chip via its labelled button', () => {
    render(<Harness type="list" initial="a, b" />);
    fireEvent.click(screen.getByRole('button', { name: 'Remove a' }));
    expect(value()).toBe('b');
  });
});

describe('VariableValueInput — secret reveal gate', () => {
  it('keeps json masked until revealed, then mounts the editor', async () => {
    render(<Harness type="json" isSecret initial="{}" />);
    // Masked: a password input, no editor.
    const masked = document.querySelector(
      'input[type="password"]',
    ) as HTMLInputElement;
    expect(masked).toBeInTheDocument();
    expect(screen.queryByLabelText('JSON value')).toBeNull();
    // Reveal → rich editor (lazy-loaded) appears.
    fireEvent.click(screen.getByRole('button', { name: 'Reveal value' }));
    expect(await screen.findByLabelText('JSON value')).toBeInTheDocument();
  });
});

describe('VariableValueInput — json validity', () => {
  it('flags invalid json and clears once it parses', async () => {
    render(<Harness type="json" />);
    const editor = await screen.findByLabelText('JSON value');
    fireEvent.change(editor, { target: { value: '{bad' } });
    expect(valid()).toBe('false');
    fireEvent.change(editor, { target: { value: '{"ok":true}' } });
    expect(valid()).toBe('true');
    expect(value()).toBe('{"ok":true}');
  });
});

describe('VariableValueInput — string', () => {
  it('emits typed text and submits on Enter', () => {
    const onSubmit = vi.fn();
    function S() {
      const [v, setV] = useState('');
      return (
        <VariableValueInput
          type="string"
          value={v}
          onChange={setV}
          isSecret={false}
          revealed
          onToggleReveal={() => {}}
          onRequestSubmit={onSubmit}
        />
      );
    }
    render(<S />);
    const input = screen.getByRole('textbox');
    fireEvent.change(input, { target: { value: 'hello' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onSubmit).toHaveBeenCalledTimes(1);
  });
});
