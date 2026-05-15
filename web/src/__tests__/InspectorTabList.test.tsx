import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { InspectorTabList, InspectorTabButton } from '../components/inspector/InspectorTabList';

function Harness({ initial = 'a' }: { initial?: 'a' | 'b' }) {
  // Active tab is static for these tests — we only assert aria + click
  // wiring; selection-state behavior is the caller's contract, not the
  // primitive's.
  return (
    <InspectorTabList ariaLabel="Sample">
      <InspectorTabButton
        active={initial === 'a'}
        onClick={vi.fn()}
        label="Alpha"
        controls="panel-a"
      />
      <InspectorTabButton
        active={initial === 'b'}
        onClick={vi.fn()}
        label="Beta"
        controls="panel-b"
      />
    </InspectorTabList>
  );
}

describe('InspectorTabList', () => {
  it('renders a tablist with the provided aria-label', () => {
    render(<Harness />);
    expect(screen.getByRole('tablist', { name: 'Sample' })).toBeInTheDocument();
  });

  it('marks the active tab with aria-selected', () => {
    render(<Harness initial="b" />);
    const tabs = screen.getAllByRole('tab');
    expect(tabs[0]).toHaveAttribute('aria-selected', 'false');
    expect(tabs[1]).toHaveAttribute('aria-selected', 'true');
  });

  it('wires aria-controls to the panel id', () => {
    render(<Harness />);
    const [alpha, beta] = screen.getAllByRole('tab');
    expect(alpha).toHaveAttribute('aria-controls', 'panel-a');
    expect(beta).toHaveAttribute('aria-controls', 'panel-b');
  });

  it('calls onClick when a tab is clicked', () => {
    const handler = vi.fn();
    render(
      <InspectorTabList ariaLabel="Sample">
        <InspectorTabButton
          active={false}
          onClick={handler}
          label="Alpha"
          controls="panel-a"
        />
      </InspectorTabList>,
    );
    fireEvent.click(screen.getByRole('tab', { name: 'Alpha' }));
    expect(handler).toHaveBeenCalledTimes(1);
  });
});
