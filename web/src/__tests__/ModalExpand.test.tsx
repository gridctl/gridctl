import { afterEach, describe, expect, it } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { Modal } from '../components/ui/Modal';

afterEach(() => cleanup());

function panel() {
  return screen.getByRole('dialog');
}

describe('Modal expand toggle', () => {
  it('steps a full-size panel up to fullscreen and back', () => {
    render(
      <Modal isOpen onClose={() => {}} title="Editor" expandable size="full">
        <div>body</div>
      </Modal>,
    );
    expect(panel().className).toContain('max-w-5xl');

    fireEvent.click(screen.getByTitle('Expanded view'));
    expect(panel().className).toContain('max-w-[96vw]');
    expect(panel().className).toContain('h-[94vh]');

    fireEvent.click(screen.getByTitle('Compact view'));
    expect(panel().className).toContain('max-w-5xl');
  });

  it('keeps the legacy default-to-wide step when no size is forced', () => {
    render(
      <Modal isOpen onClose={() => {}} title="Plain" expandable>
        <div>body</div>
      </Modal>,
    );
    expect(panel().className).toContain('max-w-lg');
    fireEvent.click(screen.getByTitle('Expanded view'));
    expect(panel().className).toContain('max-w-3xl');
  });

  it('hides the toggle when there is no larger size to expand to', () => {
    render(
      <Modal isOpen onClose={() => {}} title="Max" expandable size="fullscreen">
        <div>body</div>
      </Modal>,
    );
    expect(screen.queryByTitle('Expanded view')).toBeNull();
  });
});
