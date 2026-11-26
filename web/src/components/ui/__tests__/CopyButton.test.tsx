import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { CopyButton } from '../CopyButton';

function setExecCommand(impl: ((command: string) => boolean) | undefined) {
  Object.defineProperty(document, 'execCommand', { value: impl, configurable: true, writable: true });
}

async function click(button: HTMLElement) {
  await act(async () => {
    fireEvent.click(button);
  });
}

describe('CopyButton', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    setExecCommand(undefined);
    Object.assign(navigator, { clipboard: undefined });
  });

  it('copies the full value, shows the copied state, and reverts after 1500ms', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    render(<CopyButton value="http://localhost:9000/mcp" label="Copy endpoint" />);
    const button = screen.getByRole('button', { name: 'Copy endpoint' });
    expect(button).toHaveAttribute('type', 'button');
    expect(screen.getByRole('status')).toBeEmptyDOMElement();

    await click(button);
    expect(writeText).toHaveBeenCalledWith('http://localhost:9000/mcp');
    expect(screen.getByRole('status')).toHaveTextContent('Copied');
    expect(button).toHaveClass('text-status-running');

    act(() => vi.advanceTimersByTime(1499));
    expect(screen.getByRole('status')).toHaveTextContent('Copied');
    act(() => vi.advanceTimersByTime(1));
    expect(screen.getByRole('status')).toBeEmptyDOMElement();
    expect(button).not.toHaveClass('text-status-running');
  });

  it('shows Copy failed when the browser refuses the copy', async () => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    setExecCommand(() => false);
    render(<CopyButton value="abc" label="Copy container ID" />);
    const button = screen.getByRole('button', { name: 'Copy container ID' });
    await click(button);
    expect(button).toHaveTextContent('Copy failed');
    expect(screen.getByRole('status')).toHaveTextContent('Copy failed');
    act(() => vi.advanceTimersByTime(1500));
    expect(button).not.toHaveTextContent('Copy failed');
  });

  it.each([
    ['throws', () => { throw new Error('blocked'); }],
    ['is missing', undefined],
  ])('shows Copy failed when the legacy copy command %s', async (_case, impl) => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    setExecCommand(impl as ((command: string) => boolean) | undefined);
    render(<CopyButton value="abc" label="Copy image" />);
    await click(screen.getByRole('button', { name: 'Copy image' }));
    expect(screen.getByRole('status')).toHaveTextContent('Copy failed');
  });

  it('counts a successful legacy copy as copied', async () => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    setExecCommand(() => true);
    render(<CopyButton value="abc" label="Copy image" />);
    await click(screen.getByRole('button', { name: 'Copy image' }));
    expect(screen.getByRole('status')).toHaveTextContent('Copied');
  });

  it('replaces visible text with Copied and restores it', async () => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    render(<CopyButton value="github__search" label="Copy name">Copy name</CopyButton>);
    const button = screen.getByRole('button', { name: 'Copy name' });
    expect(button).toHaveTextContent('Copy name');
    await click(button);
    expect(button).toHaveTextContent('Copied');
    act(() => vi.advanceTimersByTime(1500));
    expect(button).toHaveTextContent('Copy name');
  });

  it('does not propagate the click', async () => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    const outer = vi.fn();
    render(
      <div onClick={outer}>
        <CopyButton value="abc" label="Copy endpoint" />
      </div>,
    );
    await click(screen.getByRole('button', { name: 'Copy endpoint' }));
    expect(outer).not.toHaveBeenCalled();
  });

  it('clears its timer on unmount', async () => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
    const { unmount } = render(<CopyButton value="abc" label="Copy endpoint" />);
    await click(screen.getByRole('button', { name: 'Copy endpoint' }));
    expect(vi.getTimerCount()).toBe(1);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
