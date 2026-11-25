import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, within } from '@testing-library/react';
import { SegmentedControl } from '../components/wizard/SegmentedControl';
import { ExpertModeToggle } from '../components/wizard/ExpertModeToggle';

const options = [
  { value: 'templates', label: 'Templates' },
  { value: 'catalog', label: 'Catalog' },
] as const;

describe('SegmentedControl', () => {
  it('renders a labeled group with exactly one pressed button', () => {
    render(<SegmentedControl ariaLabel="Server source" value="templates" onChange={() => {}} options={[...options]} />);
    const group = screen.getByRole('group', { name: 'Server source' });
    const buttons = within(group).getAllByRole('button');
    expect(buttons).toHaveLength(2);
    for (const button of buttons) expect(button).toHaveAttribute('type', 'button');
    expect(buttons.filter((b) => b.getAttribute('aria-pressed') === 'true')).toHaveLength(1);
    expect(within(group).getByRole('button', { name: 'Templates' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(group).getByRole('button', { name: 'Catalog' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('marks the selected segment with a border and weight, not color alone', () => {
    render(<SegmentedControl ariaLabel="Server source" value="templates" onChange={() => {}} options={[...options]} />);
    const selected = screen.getByRole('button', { name: 'Templates' });
    const other = screen.getByRole('button', { name: 'Catalog' });
    expect(selected.className).toMatch(/border-primary/);
    expect(selected.className).toMatch(/font-semibold/);
    expect(other.className).toMatch(/border-transparent/);
    expect(other.className).not.toMatch(/font-semibold/);
  });

  it('calls onChange with the clicked value', () => {
    const onChange = vi.fn();
    render(<SegmentedControl ariaLabel="Server source" value="templates" onChange={onChange} options={[...options]} />);
    fireEvent.click(screen.getByRole('button', { name: 'Catalog' }));
    expect(onChange).toHaveBeenCalledWith('catalog');
  });
});

describe('ExpertModeToggle', () => {
  it('is an Editor mode group that toggles with a boolean', () => {
    const onToggle = vi.fn();
    render(<ExpertModeToggle expertMode={false} onToggle={onToggle} />);
    const group = screen.getByRole('group', { name: 'Editor mode' });
    expect(within(group).getByRole('button', { name: 'Form' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(group).getByRole('button', { name: 'YAML' })).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(within(group).getByRole('button', { name: 'YAML' }));
    expect(onToggle).toHaveBeenCalledWith(true);
  });
});
