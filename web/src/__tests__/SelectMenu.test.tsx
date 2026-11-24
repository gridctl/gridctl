import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { SelectMenu, type SelectMenuItem } from '../components/detached/SelectMenu';

function items(onSelect = vi.fn()): SelectMenuItem[] {
  return [
    { key: 'a', label: 'alpha', dotClass: 'bg-primary', tag: 'MCP server', selected: true, onSelect: () => onSelect('alpha') },
    { key: 'b', label: 'beta', dotClass: 'bg-secondary', tag: 'resource', selected: false, onSelect: () => onSelect('beta') },
  ];
}

function renderMenu(list: SelectMenuItem[] = items(), emptyText?: string) {
  render(
    <div>
      <p>outside</p>
      <SelectMenu label="Node" value={<span>alpha</span>} items={list} emptyText={emptyText} />
    </div>,
  );
  return screen.getByRole('button', { name: 'alpha' });
}

describe('SelectMenu', () => {
  it('is closed by default and exposes the menu button state', () => {
    const trigger = renderMenu();
    expect(trigger).toHaveAttribute('type', 'button');
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('opens on click with labeled radio items and the current one checked', () => {
    const trigger = renderMenu();
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const menu = screen.getByRole('menu', { name: 'Node' });
    expect(menu).toBeInTheDocument();
    const [first, second] = screen.getAllByRole('menuitemradio');
    expect(first).toHaveAttribute('aria-checked', 'true');
    expect(first).toHaveAttribute('type', 'button');
    expect(first).toHaveTextContent('MCP server');
    expect(second).toHaveAttribute('aria-checked', 'false');
  });

  it('closes on Escape and returns focus to the trigger', () => {
    const trigger = renderMenu();
    fireEvent.click(trigger);
    screen.getAllByRole('menuitemradio')[1].focus();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(trigger).toHaveFocus();
  });

  it('closes on an outside mousedown without moving focus to the trigger', () => {
    const trigger = renderMenu();
    fireEvent.click(trigger);
    const item = screen.getAllByRole('menuitemradio')[1];
    item.focus();
    fireEvent.mouseDown(screen.getByText('outside'));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).not.toHaveFocus();
  });

  it('selecting an item calls back, closes, and focuses the trigger', () => {
    const onSelect = vi.fn();
    const trigger = renderMenu(items(onSelect));
    fireEvent.click(trigger);
    const beta = screen.getByRole('menuitemradio', { name: /beta/ });
    fireEvent.mouseDown(beta);
    fireEvent.click(beta);
    expect(onSelect).toHaveBeenCalledWith('beta');
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('shows the empty row when there are no items', () => {
    const trigger = renderMenu([], 'No nodes available');
    fireEvent.click(trigger);
    expect(screen.getByRole('menu')).toHaveTextContent('No nodes available');
    expect(screen.queryByRole('menuitemradio')).not.toBeInTheDocument();
  });
});
