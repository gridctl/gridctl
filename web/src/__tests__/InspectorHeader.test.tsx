import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { createRef } from 'react';
import { render, screen } from '@testing-library/react';
import { InspectorHeader } from '../components/inspector/InspectorHeader';

describe('InspectorHeader', () => {
  it('renders a plain heading without a headingRef', () => {
    render(<InspectorHeader title="search" />);
    expect(screen.getByRole('heading', { name: 'search' })).not.toHaveAttribute('tabindex');
  });

  it('makes the heading a programmatic focus target with a headingRef', () => {
    const ref = createRef<HTMLHeadingElement>();
    render(<InspectorHeader title="search" headingRef={ref} />);
    const heading = screen.getByRole('heading', { name: 'search' });
    expect(heading).toHaveAttribute('tabindex', '-1');
    expect(ref.current).toBe(heading);
  });
});
