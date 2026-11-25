import { useState } from 'react';
import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { Tab, TabPanel, Tabs, type TabsVariant } from '../components/ui/Tabs';
import { SEGMENT_GROUP_CLASS, segmentClass } from '../components/ui/segment';

const TABS = [
  { id: 'tab-one', panel: 'panel-one', label: 'One' },
  { id: 'tab-two', panel: 'panel-two', label: 'Two' },
  { id: 'tab-three', panel: 'panel-three', label: 'Three' },
] as const;

function Harness({ variant }: { variant?: TabsVariant } = {}) {
  const [active, setActive] = useState<(typeof TABS)[number]['id']>('tab-one');
  return (
    <>
      <Tabs ariaLabel="Sample" variant={variant}>
        {TABS.map((tab) => (
          <Tab
            key={tab.id}
            id={tab.id}
            selected={active === tab.id}
            controls={tab.panel}
            onSelect={() => setActive(tab.id)}
          >
            {tab.label}
          </Tab>
        ))}
      </Tabs>
      {TABS.map((tab) => (
        <TabPanel
          key={tab.panel}
          id={tab.panel}
          labelledBy={tab.id}
          active={active === tab.id}
        >
          {tab.label} panel
        </TabPanel>
      ))}
    </>
  );
}

describe('Tabs', () => {
  it('moves selection and focus with arrows, Home, and End, and wires each panel', () => {
    render(<Harness />);
    const [one, two, three] = screen.getAllByRole('tab');

    one.focus();
    fireEvent.keyDown(one, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(two);
    expect(two).toHaveAttribute('aria-selected', 'true');
    expect(one).toHaveAttribute('aria-selected', 'false');

    fireEvent.keyDown(two, { key: 'End' });
    expect(document.activeElement).toBe(three);
    expect(three).toHaveAttribute('aria-selected', 'true');

    fireEvent.keyDown(three, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(one);
    expect(one).toHaveAttribute('aria-selected', 'true');

    fireEvent.keyDown(one, { key: 'Home' });
    expect(document.activeElement).toBe(one);
    expect(one).toHaveAttribute('aria-selected', 'true');

    for (const tab of screen.getAllByRole('tab')) {
      const panelId = tab.getAttribute('aria-controls');
      expect(panelId).toBeTruthy();
      const panel = document.getElementById(panelId!);
      expect(panel).toHaveAttribute('role', 'tabpanel');
      expect(panel).toHaveAttribute('aria-labelledby', tab.id);
    }
  });

  it('keeps the default underline classes unchanged', () => {
    render(<Harness />);
    expect(screen.getByRole('tablist').className).toBe(
      'px-5 pt-3 flex items-center gap-1 border-b border-border-subtle/50',
    );
    const [one, two] = screen.getAllByRole('tab');
    expect(one.className).toBe(
      'px-3 py-1.5 -mb-px font-mono text-3xs uppercase tracking-[0.2em] border-b-2 transition-colors focus-visible:-outline-offset-2 border-primary text-text-primary',
    );
    expect(two.className).toBe(
      'px-3 py-1.5 -mb-px font-mono text-3xs uppercase tracking-[0.2em] border-b-2 transition-colors focus-visible:-outline-offset-2 border-transparent text-text-muted hover:text-text-primary',
    );
  });

  it('keeps the underline chrome for a Tab without a Tabs parent', () => {
    render(<Tab selected controls="p" onSelect={() => {}}>Solo</Tab>);
    expect(screen.getByRole('tab')).toHaveClass('border-b-2', 'border-primary');
  });

  it('applies segment classes with the segmented variant and keeps the semantics', () => {
    render(<Harness variant="segmented" />);
    const list = screen.getByRole('tablist');
    expect(list.className).toBe(SEGMENT_GROUP_CLASS);
    expect(list).not.toHaveClass('px-5', 'pt-3', 'border-b');
    const [one, two] = screen.getAllByRole('tab');
    expect(one.className).toBe(segmentClass(true));
    expect(one).toHaveClass('bg-primary/10', 'text-primary', 'rounded-md', 'text-xs');
    expect(two.className).toBe(segmentClass(false));
    expect(two).not.toHaveClass('bg-primary/10', 'uppercase', 'border-b-2');
    expect(one).toHaveAttribute('tabindex', '0');
    expect(two).toHaveAttribute('tabindex', '-1');

    one.focus();
    fireEvent.keyDown(one, { key: 'ArrowRight' });
    expect(document.activeElement).toBe(two);
    expect(two).toHaveAttribute('aria-selected', 'true');
    expect(two.className).toBe(segmentClass(true));
    expect(one.className).toBe(segmentClass(false));
  });
});
