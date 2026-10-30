import { beforeEach, describe, expect, it } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';

beforeEach(() => {
  cleanup();
  useWizardStore.getState().reset();
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

function Harness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        onClick={() => {
          useWizardStore.setState({ isOpen: true, currentStep: 'type' });
          setOpen(true);
        }}
      >
        Create Resource
      </button>
      {open && <CreationWizard />}
    </>
  );
}

describe('CreationWizard dialog semantics', () => {
  it('exposes a labeled modal dialog and a named close button', () => {
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(<CreationWizard />);
    const dialog = screen.getByRole('dialog', { name: /create new resource/i });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByRole('button', { name: /close/i })).toHaveAttribute('type', 'button');
    expect(screen.getByRole('button', { name: /start over/i })).toHaveAttribute('type', 'button');
  });

  it('returns focus to the trigger after Escape', async () => {
    render(<Harness />);
    const trigger = screen.getByRole('button', { name: /create resource/i });
    trigger.focus();
    fireEvent.click(trigger);
    await act(async () => {
      await nextFrame();
    });
    expect(screen.getByRole('dialog', { name: /create new resource/i })).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });
});
