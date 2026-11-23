import { describe, it, expect, vi } from 'vitest';
import { render } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AppShell } from '../components/shell/AppShell';

vi.mock('../components/layout/Header', () => ({ Header: () => null }));
vi.mock('../components/layout/StatusBar', () => ({ StatusBar: () => null }));
vi.mock('../components/palette/CommandPalette', () => ({ CommandPalette: () => null }));
vi.mock('../components/ui/Toast', () => ({ ToastContainer: () => null }));
vi.mock('../hooks/usePolling', () => ({ usePolling: () => ({ refresh: async () => {} }) }));
vi.mock('../hooks/useGlobalCommands', () => ({ useGlobalCommands: () => {} }));

describe('AppShell layout', () => {
  it('lets its single column shrink below the widest item', () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/stack']}>
        <Routes>
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>,
    );
    const root = container.firstElementChild as HTMLElement;
    expect(root.style.gridTemplateColumns).toBe('minmax(0, 1fr)');
  });
});
