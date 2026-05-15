import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { Activity } from 'lucide-react';
import { InspectorSection } from '../components/inspector/InspectorSection';

describe('InspectorSection', () => {
  it('starts collapsed by default and hides its content', () => {
    render(
      <InspectorSection title="Status">
        <div>panel body</div>
      </InspectorSection>,
    );
    // Body is rendered but collapsed via max-h/opacity classes — assert the
    // animated wrapper carries the "collapsed" classes rather than the open ones.
    const body = screen.getByText('panel body');
    const animatedWrapper = body.parentElement?.parentElement;
    expect(animatedWrapper?.className).toContain('max-h-0');
    expect(animatedWrapper?.className).toContain('opacity-0');
  });

  it('respects defaultOpen', () => {
    render(
      <InspectorSection title="Status" defaultOpen>
        <div>panel body</div>
      </InspectorSection>,
    );
    const body = screen.getByText('panel body');
    const animatedWrapper = body.parentElement?.parentElement;
    expect(animatedWrapper?.className).toContain('max-h-[1000px]');
    expect(animatedWrapper?.className).toContain('opacity-100');
  });

  it('toggles when the header is clicked', () => {
    render(
      <InspectorSection title="Status">
        <div>panel body</div>
      </InspectorSection>,
    );
    const body = screen.getByText('panel body');
    const animatedWrapper = body.parentElement?.parentElement;
    expect(animatedWrapper?.className).toContain('max-h-0');

    fireEvent.click(screen.getByRole('button', { name: /status/i }));
    expect(animatedWrapper?.className).toContain('max-h-[1000px]');

    fireEvent.click(screen.getByRole('button', { name: /status/i }));
    expect(animatedWrapper?.className).toContain('max-h-0');
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
