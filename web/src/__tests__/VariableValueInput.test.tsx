import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VariableValueInput } from '../components/vault/VariableValueInput';
import type { VariableType } from '../lib/api';

// CodeMirror is aliased to a textarea stub under test (see vitest.config.ts).

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

describe('VariableValueInput — secret handling', () => {
  it('always shows the json editor, even for a secret (no mask step)', async () => {
    render(<Harness type="json" isSecret initial="{}" />);
    expect(await screen.findByLabelText('JSON value')).toBeInTheDocument();
    expect(document.querySelector('input[type="password"]')).toBeNull();
  });

  it('still masks a secret string until revealed', () => {
    render(<Harness type="string" isSecret />);
    const input = screen.getByPlaceholderText('secret value');
    expect(input).toHaveAttribute('type', 'password');
    fireEvent.click(screen.getByRole('button', { name: 'Reveal value' }));
    expect(input).toHaveAttribute('type', 'text');
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
