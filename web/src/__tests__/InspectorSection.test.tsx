import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { useState } from 'react';
import { Activity } from 'lucide-react';
import { InspectorSection } from '../components/inspector/InspectorSection';

function panelFor(toggle: HTMLElement): HTMLElement | null {
  const id = toggle.getAttribute('aria-controls');
  return id ? document.getElementById(id) : null;
}

function Draft() {
  const [value, setValue] = useState('');
  return <input aria-label="draft" value={value} onChange={(e) => setValue(e.target.value)} />;
}

describe('InspectorSection', () => {
  it('starts collapsed with an empty hidden panel and renders no body', () => {
    render(
      <InspectorSection title="Status">
        <div>panel body</div>
      </InspectorSection>,
    );
    const toggle = screen.getByRole('button', { name: /status/i });
    expect(toggle).toHaveAttribute('type', 'button');
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle).toHaveAttribute('aria-controls');
    expect(screen.queryByText('panel body')).not.toBeInTheDocument();

    const panel = panelFor(toggle);
    expect(panel).toBeInTheDocument();
    expect(panel).toHaveAttribute('hidden');
    expect(panel).toBeEmptyDOMElement();
  });

  it('respects defaultOpen', () => {
    render(
      <InspectorSection title="Status" defaultOpen>
        <div>panel body</div>
      </InspectorSection>,
    );
    const toggle = screen.getByRole('button', { name: /status/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const panel = panelFor(toggle);
    expect(panel).not.toHaveAttribute('hidden');
    expect(panel).toContainElement(screen.getByText('panel body'));
  });

  it('mounts the body on first open and keeps it hidden in place after collapsing', () => {
    render(
      <InspectorSection title="Status">
        <div>panel body</div>
      </InspectorSection>,
    );
    const toggle = screen.getByRole('button', { name: /status/i });

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    const panel = panelFor(toggle);
    expect(panel).not.toHaveAttribute('hidden');
    expect(panel).toContainElement(screen.getByText('panel body'));

    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(panelFor(toggle)).toBe(panel);
    expect(panel).toHaveAttribute('hidden');
    expect(panel).toContainElement(screen.getByText('panel body'));
  });

  it('keeps child state across collapse and reopen', () => {
    render(
      <InspectorSection title="Tools">
        <Draft />
      </InspectorSection>,
    );
    const toggle = screen.getByRole('button', { name: /tools/i });
    fireEvent.click(toggle);
    fireEvent.change(screen.getByLabelText('draft'), { target: { value: 'unsaved' } });
    fireEvent.click(toggle);
    fireEvent.click(toggle);
    expect(screen.getByLabelText('draft')).toHaveValue('unsaved');
  });

  it('removes collapsed controls from the tab order and the accessibility tree', () => {
    render(
      <InspectorSection title="Availability">
        <button type="button">Disable</button>
      </InspectorSection>,
    );
    const toggle = screen.getByRole('button', { name: /availability/i });
    fireEvent.click(toggle);
    expect(screen.getByRole('button', { name: 'Disable' })).toBeInTheDocument();
    fireEvent.click(toggle);
    expect(screen.queryByRole('button', { name: 'Disable' })).not.toBeInTheDocument();
    expect(screen.getAllByRole('button')).toEqual([toggle]);
  });

  it('drops the fixed height cap and fade classes from the panel', () => {
    render(
      <InspectorSection title="Status" defaultOpen>
        <div>panel body</div>
      </InspectorSection>,
    );
    const panel = panelFor(screen.getByRole('button', { name: /status/i }));
    expect(panel?.className ?? '').not.toMatch(/max-h|opacity|overflow-hidden/);
  });

  it('renders an icon and a count badge', () => {
    render(
      <InspectorSection title="Tools" icon={Activity} count={3}>
        body
      </InspectorSection>,
    );
    expect(screen.getByText('Tools')).toBeInTheDocument();
    expect(screen.getByText('3')).toBeInTheDocument();
  });
});
