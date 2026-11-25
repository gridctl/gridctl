import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { KeyValueEditor } from '../components/wizard/KeyValueEditor';

// Mock VariablesPopover to avoid variable store API calls.
vi.mock('../components/wizard/VariablesPopover', () => ({
  VariablesPopover: ({ onSelect }: { onSelect: (ref: string) => void }) => (
    <button data-testid="variables-popover" onClick={() => onSelect('${var:TEST}')}>
      var
    </button>
  ),
}));

const LABEL = 'Environment Variables';
const DUPLICATE_DB = 'Duplicate key "DB". Only the first row is used.';

function Harness({
  initial,
  showSecrets,
  onMap,
}: {
  initial: Record<string, string>;
  showSecrets?: boolean;
  onMap?: (map: Record<string, string>) => void;
}) {
  const [map, setMap] = useState(initial);
  return (
    <KeyValueEditor
      label={LABEL}
      value={map}
      onChange={(next) => {
        setMap(next);
        onMap?.(next);
      }}
      showSecrets={showSecrets}
    />
  );
}

function renderHarness(initial: Record<string, string>, showSecrets = false) {
  let latest = initial;
  render(<Harness initial={initial} showSecrets={showSecrets} onMap={(map) => { latest = map; }} />);
  return { map: () => latest };
}

const keyInputs = () => screen.getAllByRole('textbox', { name: new RegExp(`^${LABEL} key \\d+$`) });
const keyInput = (n: number) => screen.getByRole('textbox', { name: `${LABEL} key ${n}` });
const valueInput = (n: number) => screen.getByRole('textbox', { name: `${LABEL} value ${n}` });
const clickAdd = () => fireEvent.click(screen.getByRole('button', { name: 'Add' }));

describe('KeyValueEditor', () => {
  it('keeps both rows and focus while typing a key through an existing prefix', () => {
    const harness = renderHarness({ DB: 'postgres' });
    clickAdd();
    const input = keyInput(2);
    input.focus();
    for (const text of ['D', 'DB', 'DB_', 'DB_HOST']) {
      fireEvent.change(keyInput(2), { target: { value: text } });
      expect(keyInputs()).toHaveLength(2);
      expect(valueInput(1)).toHaveValue('postgres');
      if (text === 'DB') {
        expect(keyInput(1)).toHaveAttribute('aria-invalid', 'true');
        expect(keyInput(2)).toHaveAttribute('aria-invalid', 'true');
        expect(screen.getByText(DUPLICATE_DB)).toBeInTheDocument();
        expect(harness.map()).toEqual({ DB: 'postgres' });
      }
    }
    expect(keyInput(1)).not.toHaveAttribute('aria-invalid');
    expect(keyInput(2)).not.toHaveAttribute('aria-invalid');
    expect(screen.queryByText(/Duplicate key/)).not.toBeInTheDocument();
    expect(harness.map()).toEqual({ DB: 'postgres', DB_HOST: '' });
    expect(keyInput(2)).toBe(input);
    expect(document.activeElement).toBe(input);
  });

  it('points a duplicated key input at the inline message', () => {
    renderHarness({ DB: 'postgres' });
    clickAdd();
    fireEvent.change(keyInput(2), { target: { value: 'DB' } });
    const message = screen.getByText(DUPLICATE_DB);
    for (const n of [1, 2]) {
      const describedBy = keyInput(n).getAttribute('aria-describedby');
      expect(describedBy).toBeTruthy();
      expect(document.getElementById(describedBy ?? '')).toBe(message);
    }
    expect(keyInput(1).className).toContain('border-status-error/50');
  });

  it('keeps a value typed before the key collides', () => {
    const harness = renderHarness({ DB: 'postgres' });
    clickAdd();
    fireEvent.change(valueInput(2), { target: { value: 'localhost' } });
    fireEvent.change(keyInput(2), { target: { value: 'DB' } });
    expect(valueInput(1)).toHaveValue('postgres');
    expect(valueInput(2)).toHaveValue('localhost');
    expect(harness.map()).toEqual({ DB: 'postgres' });
  });

  it('adds a row on every Add click', () => {
    renderHarness({});
    expect(screen.getByText('No entries')).toBeInTheDocument();
    clickAdd();
    clickAdd();
    expect(keyInputs()).toHaveLength(2);
    expect(screen.getAllByRole('button', { name: 'Remove entry' })).toHaveLength(2);
    expect(screen.queryByText('No entries')).not.toBeInTheDocument();
  });

  it('does not move or rename rows when a key is all digits', () => {
    renderHarness({ HOST: 'h' });
    clickAdd();
    fireEvent.change(keyInput(2), { target: { value: '8' } });
    fireEvent.change(keyInput(2), { target: { value: '80' } });
    expect(keyInput(1)).toHaveValue('HOST');
    expect(valueInput(1)).toHaveValue('h');
    expect(keyInput(2)).toHaveValue('80');
  });

  it('keeps a __proto__ key as a row', () => {
    const harness = renderHarness({});
    clickAdd();
    fireEvent.change(keyInput(1), { target: { value: '__proto__' } });
    expect(keyInput(1)).toHaveValue('__proto__');
    expect(Object.keys(harness.map())).toEqual(['__proto__']);
  });

  it('removes only the clicked duplicate row and clears the flag', () => {
    const harness = renderHarness({ DB: 'postgres' });
    clickAdd();
    fireEvent.change(valueInput(2), { target: { value: 'localhost' } });
    fireEvent.change(keyInput(2), { target: { value: 'DB' } });
    const removes = screen.getAllByRole('button', { name: 'Remove DB' });
    expect(removes).toHaveLength(2);
    fireEvent.click(removes[1]);
    expect(keyInputs()).toHaveLength(1);
    expect(valueInput(1)).toHaveValue('postgres');
    expect(keyInput(1)).not.toHaveAttribute('aria-invalid');
    expect(screen.queryByText(/Duplicate key/)).not.toBeInTheDocument();
    expect(harness.map()).toEqual({ DB: 'postgres' });
  });

  it('shows the entries of a map replaced from outside', () => {
    const { rerender } = render(<KeyValueEditor label={LABEL} value={{ A: '1' }} onChange={vi.fn()} />);
    expect(keyInput(1)).toHaveValue('A');
    rerender(<KeyValueEditor label={LABEL} value={{ B: '2', C: '3' }} onChange={vi.fn()} />);
    expect(keyInputs()).toHaveLength(2);
    expect(keyInput(1)).toHaveValue('B');
    expect(valueInput(2)).toHaveValue('3');
  });

  it('sets only the selected row value from the variables popover', () => {
    const harness = renderHarness({ A: 'one', B: 'two' }, true);
    fireEvent.click(screen.getAllByTestId('variables-popover')[1]);
    expect(valueInput(1)).toHaveValue('one');
    expect(valueInput(2)).toHaveValue('${var:TEST}');
    expect(harness.map()).toEqual({ A: 'one', B: '${var:TEST}' });
  });

  it('labels the row group with the editor label', () => {
    renderHarness({ A: '1' });
    expect(screen.getByRole('group', { name: LABEL })).toContainElement(keyInput(1));
  });
});
