import { describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { AppRoutes } from '../routes';

vi.mock('../pages/DetachedLogsPage', () => ({
  DetachedLogsPage: () => <div>logs popout</div>,
}));
vi.mock('../pages/DetachedSidebarPage', () => ({
  DetachedSidebarPage: () => <div>sidebar popout</div>,
}));
vi.mock('../pages/DetachedEditorPage', () => ({
  DetachedEditorPage: () => <div>editor popout</div>,
}));
vi.mock('../pages/DetachedRegistryPage', () => ({
  DetachedRegistryPage: () => <div>registry popout</div>,
}));
vi.mock('../pages/DetachedMetricsPage', () => ({
  DetachedMetricsPage: () => <div>metrics popout</div>,
}));
vi.mock('../pages/DetachedTracesPage', () => ({
  DetachedTracesPage: () => <div>traces popout</div>,
}));
vi.mock('../themes/useThemeSync', () => ({ useThemeSync: () => {} }));
vi.mock('../components/shell/AppShell', () => ({ AppShell: () => null }));

describe('lazy popout routes', () => {
  it.each([
    ['/logs-window', 'logs popout'],
    ['/sidebar', 'sidebar popout'],
    ['/editor', 'editor popout'],
    ['/library-window', 'registry popout'],
    ['/metrics-window', 'metrics popout'],
    ['/traces-window', 'traces popout'],
  ])('resolves %s', async (path, label) => {
    render(
      <MemoryRouter initialEntries={[path]}>
        <AppRoutes />
      </MemoryRouter>,
    );
    expect(await screen.findByText(label)).toBeInTheDocument();
  });
});
