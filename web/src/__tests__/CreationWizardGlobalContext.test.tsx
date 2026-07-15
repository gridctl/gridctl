import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';

beforeEach(() => {
  cleanup();
  useWizardStore.getState().reset();
  useWizardStore.setState({ isOpen: true, currentStep: 'type' });
});

describe('CreationWizard Global Context tile', () => {
  it('shows the tile on the resource type step', () => {
    render(<CreationWizard />);
    expect(screen.getByText('Global Context')).toBeInTheDocument();
    expect(screen.getByText('One AGENTS.md synced to every linked client')).toBeInTheDocument();
  });

  it('closes the wizard and opens the Global Context surface on select', () => {
    const onOpenGlobalContext = vi.fn();
    render(<CreationWizard onOpenGlobalContext={onOpenGlobalContext} />);

    fireEvent.click(screen.getByText('Global Context'));

    expect(onOpenGlobalContext).toHaveBeenCalledTimes(1);
    expect(useWizardStore.getState().isOpen).toBe(false);
  });
});
