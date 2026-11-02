import { describe, expect, it } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { Modal } from '../components/ui/Modal';
import { PopoutButton } from '../components/ui/PopoutButton';

describe('Modal header buttons', () => {
  it('names close, expand, and popout and sets type="button"', () => {
    render(
      <Modal isOpen onClose={() => undefined} title="Editor" expandable onPopout={() => undefined}>
        <div>body</div>
      </Modal>,
    );
    for (const name of [/close/i, /expanded view/i, /open in new window/i]) {
      expect(screen.getByRole('button', { name })).toHaveAttribute('type', 'button');
    }
  });

  it('keeps the popout button enabled and names the detached state', () => {
    render(
      <Modal isOpen onClose={() => undefined} title="Editor" popoutDetached onPopout={() => undefined}>
        <div>body</div>
      </Modal>,
    );
    const popout = screen.getByRole('button', { name: 'Detached. Click to focus' });
    expect(popout).toBeEnabled();
    expect(popout).toHaveAttribute('title', 'Detached. Click to focus');
  });
});

describe('PopoutButton', () => {
  it('uses the detached label and stays enabled', () => {
    render(<PopoutButton onClick={() => undefined} detached />);
    const button = screen.getByRole('button', { name: 'Detached. Click to focus' });
    expect(button).toBeEnabled();
    expect(button).toHaveAttribute('title', 'Detached. Click to focus');
  });
});
