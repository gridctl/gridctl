import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router';
import './index.css';
import { migratePreferences } from './lib/preferences';
import { CommandRegistryProvider } from './hooks/useCommandRegistry';
import { ErrorBoundary } from './components/ui/ErrorBoundary';
import { AppRoutes } from './routes';

migratePreferences();

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    {/* Last-resort net: a throw above the shell (router, providers) still
        renders a recoverable page instead of a blank #root. */}
    <ErrorBoundary variant="window">
      <BrowserRouter>
        <CommandRegistryProvider>
          <AppRoutes />
        </CommandRegistryProvider>
      </BrowserRouter>
    </ErrorBoundary>
  </StrictMode>,
);
