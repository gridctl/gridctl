import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { SlideOver } from '../components/ui/SlideOver';

beforeEach(() => cleanup());

describe('SlideOver', () => {
  it('renders title and children when open', () => {
    render(
      <SlideOver isOpen onClose={() => {}} title="Access editor">
        <p>body content</p>
      </SlideOver>,
    );
    expect(screen.getByText('Access editor')).toBeInTheDocument();
    expect(screen.getByText('body content')).toBeInTheDocument();
  });

  it('renders nothing when closed', () => {
    const { container } = render(
      <SlideOver isOpen={false} onClose={() => {}} title="Access editor">
        <p>body content</p>
      </SlideOver>,
    );
    expect(container.firstChild).toBeNull();
  });

  it('is a non-modal dialog (no aria-modal, so the canvas stays interactive)', () => {
    render(
      <SlideOver isOpen onClose={() => {}} title="Access editor">
        <p>body</p>
      </SlideOver>,
    );
    const dialog = screen.getByRole('dialog');
    expect(dialog).not.toHaveAttribute('aria-modal');
  });

  it('closes on Escape', () => {
    const onClose = vi.fn();
    render(
      <SlideOver isOpen onClose={onClose} title="Access editor">
        <p>body</p>
      </SlideOver>,
    );
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('closes via the close button', () => {
    const onClose = vi.fn();
    render(
      <SlideOver isOpen onClose={onClose} title="Access editor">
        <p>body</p>
      </SlideOver>,
    );
    fireEvent.click(screen.getByLabelText('Close access editor'));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('keeps Escape from reaching the shell on window', () => {
    const onClose = vi.fn();
    const onWindow = vi.fn();
    window.addEventListener('keydown', onWindow);
    render(
      <SlideOver isOpen onClose={onClose} title="Access editor">
        <button type="button">Grant</button>
      </SlideOver>,
    );
    fireEvent.keyDown(screen.getByRole('button', { name: 'Grant' }), { key: 'Escape' });
    expect(onClose).toHaveBeenCalledOnce();
    expect(onWindow).not.toHaveBeenCalled();
    window.removeEventListener('keydown', onWindow);
  });

  it('blurs a field on Escape and stays open', () => {
    const onClose = vi.fn();
    render(
      <SlideOver isOpen onClose={onClose} title="Access editor">
        <input aria-label="Filter" />
      </SlideOver>,
    );
    const field = screen.getByLabelText('Filter');
    field.focus();
    fireEvent.keyDown(field, { key: 'Escape' });
    expect(field).not.toHaveFocus();
    expect(onClose).not.toHaveBeenCalled();
  });
});
