import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { Button } from '../Button';

describe('Button', () => {
  it('renders the danger variant as a flat danger fill under white text', () => {
    render(<Button variant="danger">Delete</Button>);
    const button = screen.getByRole('button', { name: 'Delete' });
    expect(button).toHaveClass('bg-danger', 'text-white', 'hover:bg-danger-hover');
    expect(button.className).not.toMatch(/bg-gradient|from-|to-status-error|bg-danger\/\d+|status-error/);
  });
});
