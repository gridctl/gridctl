import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, fireEvent } from '@testing-library/react';
import { Ellipsis, FileCog, LogOut, RefreshCw } from 'lucide-react';
import { HeaderMenu, type HeaderMenuItem } from '../components/shell/HeaderMenu';

function items(overrides: Partial<Record<string, Partial<HeaderMenuItem>>> = {}): HeaderMenuItem[] {
  return [
    { key: 'a', label: 'Alpha', icon: RefreshCw, onSelect: vi.fn(), ...overrides.a },
    { key: 'b', label: 'Beta', icon: FileCog, onSelect: vi.fn(), ...overrides.b },
    { key: 'c', label: 'Gamma', icon: LogOut, onSelect: vi.fn(), separatorBefore: true, ...overrides.c },
  ];
}

function renderMenu(list: HeaderMenuItem[]) {
  render(
    <div>
      <HeaderMenu tooltip="More actions" menuLabel="More actions" icon={Ellipsis} items={list} />
      <span>outside</span>
    </div>,
  );
  return screen.getByRole('button', { name: 'More actions' });
}

function key(k: string) {
  fireEvent.keyDown(document.activeElement!, { key: k });
}

describe('HeaderMenu', () => {
  it('focuses the first item on open and moves with arrows, Home, and End', () => {
    const trigger = renderMenu(items());
    fireEvent.click(trigger);
    expect(screen.getByRole('menu', { name: 'More actions' })).toBeInTheDocument();
    const [alpha, beta, gamma] = screen.getAllByRole('menuitem');
    expect(alpha).toHaveFocus();
    key('ArrowDown');
    expect(beta).toHaveFocus();
    key('ArrowDown');
    expect(gamma).toHaveFocus();
    key('ArrowDown');
    expect(alpha).toHaveFocus();
    key('ArrowUp');
    expect(gamma).toHaveFocus();
    key('Home');
    expect(alpha).toHaveFocus();
    key('End');
    expect(gamma).toHaveFocus();
  });

  it('skips disabled items for initial focus and arrow keys', () => {
    const trigger = renderMenu(items({ a: { disabled: true } }));
    fireEvent.click(trigger);
    const [alpha, beta, gamma] = screen.getAllByRole('menuitem');
    expect(alpha).toBeDisabled();
    expect(beta).toHaveFocus();
    key('ArrowDown');
    expect(gamma).toHaveFocus();
    key('ArrowDown');
    expect(beta).toHaveFocus();
    key('ArrowUp');
    expect(gamma).toHaveFocus();
    key('Home');
    expect(beta).toHaveFocus();
  });

  it('closes on Escape and returns focus to the trigger', () => {
    const trigger = renderMenu(items());
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(trigger).toHaveFocus();
  });

  it('keeps focus inside the menu when every item is disabled', () => {
    const trigger = renderMenu(items({ a: { disabled: true }, b: { disabled: true }, c: { disabled: true } }));
    fireEvent.click(trigger);
    const menu = screen.getByRole('menu', { name: 'More actions' });
    expect(menu).toContainElement(document.activeElement as HTMLElement);
    expect(document.activeElement).toHaveAttribute('tabindex', '-1');
    key('ArrowDown');
    expect(menu).toContainElement(document.activeElement as HTMLElement);
    fireEvent.keyDown(document.activeElement!, { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('closes the menu and then runs the chosen action', () => {
    const list = items();
    const trigger = renderMenu(list);
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Beta' }));
    expect(list[1].onSelect).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('toggles closed on a second trigger click', () => {
    const trigger = renderMenu(items());
    fireEvent.click(trigger);
    fireEvent.mouseDown(trigger);
    fireEvent.click(trigger);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('closes on an outside click without moving focus to the trigger', () => {
    const trigger = renderMenu(items());
    fireEvent.click(trigger);
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).not.toHaveFocus();
  });

  it('renders a separator, a trailing hint, and a spinning item icon', () => {
    const trigger = renderMenu(items({ a: { hint: <kbd>X</kbd>, spinning: true } }));
    fireEvent.click(trigger);
    expect(screen.getByRole('separator')).toBeInTheDocument();
    const alpha = screen.getByRole('menuitem', { name: 'Alpha' });
    expect(alpha).toHaveTextContent('X');
    expect(alpha.querySelector('svg')).toHaveClass('animate-spin');
    expect(alpha).toHaveClass('min-h-8');
  });

  it('renders a link item that opens in a new tab and closes the menu on click', () => {
    const trigger = renderMenu([
      { key: 'docs', label: 'Docs', icon: FileCog, href: 'https://example.test/docs' },
    ]);
    fireEvent.click(trigger);
    const link = screen.getByRole('menuitem', { name: 'Docs' });
    expect(link.tagName).toBe('A');
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noreferrer');
    expect(link).toHaveFocus();
    const event = new MouseEvent('click', { bubbles: true, cancelable: true });
    act(() => {
      link.dispatchEvent(event);
    });
    expect(event.defaultPrevented).toBe(false);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });
});
