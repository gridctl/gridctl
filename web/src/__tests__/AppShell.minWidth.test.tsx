import { describe, it, expect, vi, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AppShell } from '../components/shell/AppShell';

vi.mock('../components/layout/Header', () => ({ Header: () => null }));
vi.mock('../components/layout/StatusBar', () => ({ StatusBar: () => null }));
vi.mock('../components/palette/CommandPalette', () => ({ CommandPalette: () => null }));
vi.mock('../components/ui/Toast', () => ({ ToastContainer: () => null }));
vi.mock('../hooks/usePolling', () => ({ usePolling: () => ({ refresh: async () => {} }) }));
vi.mock('../hooks/useGlobalCommands', () => ({ useGlobalCommands: () => {} }));

const DEFAULT_WIDTH = window.innerWidth;
let mounts = 0;

function Workspace() {
  const [draft, setDraft] = useState('');
  const [mountId] = useState(() => ++mounts);
  return (
    <div>
      <p>Workspace content</p>
      <p data-testid="mount-id">{mountId}</p>
      <input aria-label="Draft" value={draft} onChange={(e) => setDraft(e.target.value)} />
    </div>
  );
}

function resize(width: number) {
  act(() => {
    window.innerWidth = width;
    window.dispatchEvent(new Event('resize'));
  });
}

function renderShell(width: number) {
  window.innerWidth = width;
  const utils = render(
    <MemoryRouter initialEntries={['/library']}>
      <Routes>
        <Route path="/" element={<AppShell />}>
          <Route path="*" element={<Workspace />} />
        </Route>
      </Routes>
    </MemoryRouter>,
  );
  const wrapper = utils.container.querySelector('main > div.contents') as HTMLDivElement;
  return { ...utils, wrapper };
}

const heading = () => screen.queryByRole('heading', { name: 'Window too narrow' });

afterEach(() => {
  window.innerWidth = DEFAULT_WIDTH;
  document.querySelectorAll('[data-test-modal]').forEach((el) => el.remove());
});

describe('AppShell minimum width state', () => {
  it('never shows at 1024 and keeps the wrapper live', () => {
    const { wrapper } = renderShell(1024);
    expect(heading()).toBeNull();
    expect(wrapper).toBeInTheDocument();
    expect(wrapper).not.toHaveAttribute('inert');
    expect(screen.getByText('Workspace content')).toBeInTheDocument();
  });

  it('shows below 480 px and Continue anyway dismisses it until reload', () => {
    const { wrapper } = renderShell(400);
    const h = heading();
    expect(h).toBeInTheDocument();
    expect(h!.closest('section')).toHaveTextContent('480 px');
    expect(wrapper).toHaveAttribute('inert');
    expect(screen.getByText('Workspace content')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Continue anyway' }));
    expect(heading()).toBeNull();
    expect(wrapper).not.toHaveAttribute('inert');
    const main = document.querySelector('main')!;
    expect(document.activeElement).toBe(main);
    expect(main).toHaveAttribute('tabindex', '-1');

    // A lingering tabindex would let clicks on blank workspace area focus
    // <main>, which takes list navigation keys away from the page.
    act(() => main.blur());
    expect(main).not.toHaveAttribute('tabindex');

    resize(390);
    expect(heading()).toBeNull();
  });

  it('leaves <main> unfocusable until Continue anyway moves focus there', () => {
    renderShell(1024);
    expect(document.querySelector('main')).not.toHaveAttribute('tabindex');
  });

  it('clears on widening without a click and never remounts the workspace', () => {
    mounts = 0;
    const { wrapper } = renderShell(1024);
    fireEvent.change(screen.getByLabelText('Draft'), { target: { value: 'unsaved' } });

    resize(400);
    expect(heading()).toBeInTheDocument();
    expect(wrapper).toHaveAttribute('inert');

    resize(800);
    expect(heading()).toBeNull();
    expect(wrapper).not.toHaveAttribute('inert');
    expect(screen.getByTestId('mount-id')).toHaveTextContent('1');
    expect(mounts).toBe(1);
    expect(screen.getByLabelText('Draft')).toHaveValue('unsaved');
  });

  it('does not rise over an open dialog', () => {
    const modal = document.createElement('div');
    modal.setAttribute('aria-modal', 'true');
    modal.setAttribute('data-test-modal', '');
    document.body.appendChild(modal);
    const { wrapper } = renderShell(400);
    expect(heading()).toBeNull();
    expect(wrapper).not.toHaveAttribute('inert');
  });

  it('gives way to a dialog opened inside the workspace, but not to one outside it', async () => {
    const { wrapper } = renderShell(400);
    expect(heading()).toBeInTheDocument();

    const palette = document.createElement('div');
    palette.setAttribute('aria-modal', 'true');
    palette.setAttribute('data-test-modal', '');
    document.body.appendChild(palette);
    await act(async () => {});
    expect(heading()).toBeInTheDocument();
    palette.remove();

    const dialog = document.createElement('div');
    dialog.setAttribute('aria-modal', 'true');
    const field = document.createElement('input');
    field.setAttribute('aria-label', 'Skill name');
    dialog.appendChild(field);
    act(() => {
      wrapper.firstElementChild!.appendChild(dialog);
    });
    await waitFor(() => expect(heading()).toBeNull());
    expect(wrapper).not.toHaveAttribute('inert');
    expect(field).toHaveFocus();

    act(() => {
      dialog.remove();
    });
    await act(async () => {});
    expect(heading()).toBeNull();

    resize(390);
    expect(heading()).toBeInTheDocument();
  });
});
