import { describe, it, expect } from 'vitest';
import { render, screen, fireEvent, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { CanvasLegend } from '../components/graph/CanvasLegend';
import { GLYPH_META } from '../lib/graph/serverState';
import { NODE_STATUS_LABELS } from '../lib/nodeStatus';

describe('CanvasLegend', () => {
  it('opens a dialog listing every status, glyph, and counter, and closes on Escape', () => {
    render(<CanvasLegend />);
    const trigger = screen.getByRole('button', { name: 'Legend' });
    expect(trigger).toHaveAttribute('aria-haspopup', 'dialog');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');

    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const dialog = screen.getByRole('dialog', { name: 'Canvas legend' });

    for (const label of Object.values(NODE_STATUS_LABELS)) {
      expect(within(dialog).getByRole('img', { name: label })).toBeInTheDocument();
      expect(within(dialog).getByText(label)).toBeInTheDocument();
    }
    for (const { label, description } of Object.values(GLYPH_META)) {
      expect(within(dialog).getByText(label)).toBeInTheDocument();
      expect(within(dialog).getByText(description)).toBeInTheDocument();
    }
    expect(within(dialog).getByText('Tools exposed')).toBeInTheDocument();
    expect(within(dialog).getByText('Replicas')).toBeInTheDocument();
    expect(within(dialog).getByText('Autoscale replicas, current of target')).toBeInTheDocument();
    expect(within(dialog).getByText('Scaling up or down')).toBeInTheDocument();
    expect(dialog.querySelector('[class*="animate-pulse"]')).toBeNull();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Canvas legend' })).not.toBeInTheDocument();
  });
});
