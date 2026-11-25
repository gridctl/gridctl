import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';
import { Layers } from 'lucide-react';
import { Section } from '../components/wizard/Section';

function Harness({ initial = false }: { initial?: boolean }) {
  const [expanded, setExpanded] = useState(initial);
  return (
    <Section title="Identity" icon={Layers} expanded={expanded} onToggle={() => setExpanded((v) => !v)}>
      <p>Section body</p>
    </Section>
  );
}

describe('Section', () => {
  it('reports collapsed without pointing at a missing panel', () => {
    render(<Harness />);
    const header = screen.getByRole('button', { name: /identity/i });
    expect(header).toHaveAttribute('type', 'button');
    expect(header).toHaveAttribute('aria-expanded', 'false');
    expect(header).not.toHaveAttribute('aria-controls');
    expect(screen.queryByText('Section body')).not.toBeInTheDocument();
  });

  it('reports expanded and controls the panel holding its children', () => {
    render(<Harness />);
    const header = screen.getByRole('button', { name: /identity/i });
    fireEvent.click(header);
    expect(header).toHaveAttribute('aria-expanded', 'true');
    const panelId = header.getAttribute('aria-controls');
    expect(panelId).toBeTruthy();
    const panel = document.getElementById(panelId!);
    expect(panel).toContainElement(screen.getByText('Section body'));

    fireEvent.click(header);
    expect(header).toHaveAttribute('aria-expanded', 'false');
    expect(header).not.toHaveAttribute('aria-controls');
  });
});
