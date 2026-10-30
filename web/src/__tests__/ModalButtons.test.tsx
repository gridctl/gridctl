import { describe, expect, it } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { Modal } from '../components/ui/Modal';

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
});
