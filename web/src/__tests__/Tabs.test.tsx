import { useState } from 'react';
import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { Tab, TabPanel, Tabs } from '../components/ui/Tabs';

const TABS = [
  { id: 'tab-one', panel: 'panel-one', label: 'One' },
  { id: 'tab-two', panel: 'panel-two', label: 'Two' },
  { id: 'tab-three', panel: 'panel-three', label: 'Three' },
] as const;

function Harness() {
  const [active, setActive] = useState<(typeof TABS)[number]['id']>('tab-one');
  return (
    <>
      <Tabs ariaLabel="Sample">
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
});