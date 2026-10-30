import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useFocusTrap } from '../hooks/useFocusTrap';
import { VaultSetSelector } from '../components/wizard/VaultSetSelector';

vi.mock('../lib/api', () => ({
  fetchVariableSets: vi.fn().mockResolvedValue([{ name: 'dev', count: 1 }]),
}));

beforeEach(() => {
  cleanup();
  Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
    configurable: true,
    get() {
      return this.parentNode;
    },
  });
});

function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()));
}

function TrappedSelector() {
  const panelRef = useFocusTrap<HTMLDivElement>({ active: true });
  return (
    <div ref={panelRef}>
      <button type="button">First</button>
      <VaultSetSelector value={[]} onChange={() => undefined} />
    </div>
  );
}

describe('VaultSetSelector inside a focus trap', () => {
  it('keeps Tab inside the portaled dropdown', async () => {
    render(<TrappedSelector />);
    await act(async () => {
      await nextFrame();
    });
    fireEvent.click(screen.getByRole('button', { name: /add set/i }));
    const filter = screen.getByPlaceholderText('Filter sets...');
    filter.focus();
    fireEvent.keyDown(filter, { key: 'Tab' });
    expect(filter).toHaveFocus();
    expect(screen.getByRole('button', { name: 'First' })).not.toHaveFocus();
  });

  it('closes on Escape and returns focus to the trigger', async () => {
    render(<TrappedSelector />);
    await act(async () => {
      await nextFrame();
    });
    const trigger = screen.getByRole('button', { name: /add set/i });
    fireEvent.click(trigger);
    fireEvent.keyDown(screen.getByPlaceholderText('Filter sets...'), { key: 'Escape' });
    expect(screen.queryByPlaceholderText('Filter sets...')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });
});
