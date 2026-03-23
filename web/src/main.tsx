import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import './index.css';
import App from './App.tsx';
import { DetachedLogsPage } from './pages/DetachedLogsPage.tsx';
import { DetachedSidebarPage } from './pages/DetachedSidebarPage.tsx';
import { DetachedEditorPage } from './pages/DetachedEditorPage.tsx';
import { DetachedRegistryPage } from './pages/DetachedRegistryPage.tsx';
import { DetachedWorkflowPage } from './pages/DetachedWorkflowPage.tsx';
import { DetachedMetricsPage } from './pages/DetachedMetricsPage.tsx';
import { DetachedVaultPage } from './pages/DetachedVaultPage.tsx';
import { DetachedTracesPage } from './pages/DetachedTracesPage.tsx';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />} />
        <Route path="/logs" element={<DetachedLogsPage />} />
        <Route path="/sidebar" element={<DetachedSidebarPage />} />
        <Route path="/editor" element={<DetachedEditorPage />} />
        <Route path="/registry" element={<DetachedRegistryPage />} />
        <Route path="/workflow" element={<DetachedWorkflowPage />} />
        <Route path="/metrics" element={<DetachedMetricsPage />} />
        <Route path="/vault" element={<DetachedVaultPage />} />
        <Route path="/traces" element={<DetachedTracesPage />} />
      </Routes>
    </BrowserRouter>
  </StrictMode>
);
