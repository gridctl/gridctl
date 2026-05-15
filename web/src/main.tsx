import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import './index.css';
import { CommandRegistryProvider } from './hooks/useCommandRegistry';
import { AppRoutes } from './routes';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <CommandRegistryProvider>
        <AppRoutes />
      </CommandRegistryProvider>
    </BrowserRouter>
  </StrictMode>,
);
