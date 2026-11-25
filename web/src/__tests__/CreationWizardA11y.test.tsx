import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CreationWizard } from '../components/wizard/CreationWizard';
import { useWizardStore } from '../stores/useWizardStore';
import { useStackStore } from '../stores/useStackStore';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  fetchWizardDrafts: vi.fn().mockResolvedValue([]),
  fetchCatalog: vi.fn().mockResolvedValue({ servers: [] }),
}));

beforeEach(() => {
  cleanup();
  useWizardStore.getState().reset();
  useStackStore.setState({ stackName: 'demo' });
  Object.defineProperty(HTMLElement.prototype, 'offsetParent', {
    configurable: true,
    get() {
      return this.parentNode;
    },
  });
});

function nextFrame(): Promise<void> {
  return new Promise((resolve) => requestAnimationFrame(() => resolve()));
}

function Harness() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button
        type="button"
        onClick={() => {
          useWizardStore.setState({ isOpen: true, currentStep: 'type' });
          setOpen(true);
        }}
      >
        Create Resource
      </button>
      {open && <CreationWizard />}
    </>
  );
}

describe('CreationWizard dialog semantics', () => {
  it('exposes a labeled modal dialog and a named close button', () => {
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(<CreationWizard />);
    const dialog = screen.getByRole('dialog', { name: /^Create new$/ });
    expect(dialog).toHaveAttribute('aria-modal', 'true');
    expect(screen.getByRole('button', { name: /close/i })).toHaveAttribute('type', 'button');
    expect(screen.getByRole('button', { name: /start over/i })).toHaveAttribute('type', 'button');
  });

  it('returns focus to the trigger after Escape', async () => {
    render(<Harness />);
    const trigger = screen.getByRole('button', { name: /create resource/i });
    trigger.focus();
    fireEvent.click(trigger);
    await act(async () => {
      await nextFrame();
    });
    expect(screen.getByRole('dialog', { name: /^Create new$/ })).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });
});

describe('CreationWizard type picker', () => {
  it('gates the MCP Server card with visible text when there is no stack', () => {
    useStackStore.setState({ stackName: '' });
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(<CreationWizard />);
    const card = screen.getByRole('button', { name: /^MCP Server/ });
    expect(card).toHaveAttribute('aria-disabled', 'true');
    expect(card).not.toHaveAttribute('title');
    expect(card).not.toHaveAttribute('aria-pressed');
    expect(card).toHaveTextContent('Requires an active stack. Create a Stack first.');
    fireEvent.click(card);
    expect(useWizardStore.getState().currentStep).toBe('type');
  });

  it('enables the MCP Server card when a stack is active', () => {
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(<CreationWizard />);
    const card = screen.getByRole('button', { name: /^MCP Server/ });
    expect(card).not.toHaveAttribute('aria-disabled');
    expect(card).toHaveAttribute('aria-pressed', 'false');
  });
});

describe('CreationWizard step text and focus', () => {
  it('reads the step as text and focuses it on a step change, not on open', async () => {
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(<CreationWizard />);
    const first = screen.getByText('Step 1 of 4: Resource Type');
    expect(first).not.toHaveFocus();
    fireEvent.click(screen.getByRole('button', { name: /^Stack/ }));
    const second = screen.getByText('Step 2 of 4: Template');
    expect(second).toHaveFocus();
  });

  it('exposes pressed state on the server source and editor mode groups', async () => {
    useWizardStore.setState({ isOpen: true, selectedType: 'mcp-server', currentStep: 'template' });
    render(<CreationWizard />);
    const source = screen.getByRole('group', { name: 'Server source' });
    const templates = within(source).getByRole('button', { name: /Templates/ });
    const catalog = within(source).getByRole('button', { name: /Catalog/ });
    expect(templates).toHaveAttribute('aria-pressed', 'true');
    expect(catalog).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(catalog);
    expect(catalog).toHaveAttribute('aria-pressed', 'true');
    expect(templates).toHaveAttribute('aria-pressed', 'false');
  });

  it('exposes pressed and expanded state on the MCP server form', () => {
    useWizardStore.setState({ isOpen: true, selectedType: 'mcp-server', currentStep: 'form' });
    render(<CreationWizard />);
    const mode = screen.getByRole('group', { name: 'Editor mode' });
    expect(within(mode).getByRole('button', { name: /Form/ })).toHaveAttribute('aria-pressed', 'true');
    expect(within(mode).getByRole('button', { name: /YAML/ })).toHaveAttribute('aria-pressed', 'false');
    const types = screen.getByRole('group', { name: 'Server type' });
    const pressed = within(types)
      .getAllByRole('button')
      .filter((b) => b.getAttribute('aria-pressed') === 'true');
    expect(pressed).toHaveLength(1);
    expect(screen.getByRole('button', { name: /Identity/ })).toHaveAttribute('aria-expanded');
  });

  it('shows only the import indicator in the skill flow and keeps focus in the URL field', async () => {
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(
      <MemoryRouter>
        <CreationWizard />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole('button', { name: /^Skill/ }));
    expect(screen.queryByText(/Step \d of 4: Configure/)).not.toBeInTheDocument();
    expect(screen.getAllByText('Configure')).toHaveLength(1);
    expect(document.querySelectorAll('[aria-current="step"]')).toHaveLength(1);
    await act(async () => {
      await nextFrame();
    });
    expect(screen.getByPlaceholderText('https://github.com/owner/repo')).toHaveFocus();
  });
});

describe('CreationWizard draft popover', () => {
  it('closes only the Save popover on Escape', async () => {
    useWizardStore.setState({ isOpen: true, selectedType: 'stack', currentStep: 'form' });
    render(<CreationWizard />);
    const trigger = screen.getByRole('button', { name: /^Save/ });
    fireEvent.click(trigger);
    const input = screen.getByLabelText('Draft name');
    input.focus();
    fireEvent.keyDown(input, { key: 'Escape' });
    expect(screen.queryByLabelText('Draft name')).not.toBeInTheDocument();
    expect(screen.getByRole('dialog', { name: /New Stack/ })).toBeInTheDocument();
    await waitFor(() => expect(trigger).toHaveFocus());
  });
});
