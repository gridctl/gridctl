import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { createRef } from 'react';
import { render, screen } from '@testing-library/react';
import { Ellipsis } from 'lucide-react';
import { IconButton } from '../components/ui/IconButton';

describe('IconButton popup props', () => {
  it('omits aria-haspopup and aria-expanded when the props are not given', () => {
    render(<IconButton icon={Ellipsis} tooltip="Plain" />);
    const button = screen.getByRole('button', { name: 'Plain' });
    expect(button).not.toHaveAttribute('aria-haspopup');
    expect(button).not.toHaveAttribute('aria-expanded');
  });

  it('renders aria-haspopup and aria-expanded and forwards its ref', () => {
    const ref = createRef<HTMLButtonElement>();
    render(<IconButton ref={ref} icon={Ellipsis} tooltip="More" hasPopup="menu" expanded={false} />);
    const button = screen.getByRole('button', { name: 'More' });
    expect(button).toHaveAttribute('aria-haspopup', 'menu');
    expect(button).toHaveAttribute('aria-expanded', 'false');
    expect(ref.current).toBe(button);
  });
});
