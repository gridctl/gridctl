import { StrictMode, lazy, Suspense } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import './index.css';
import App from './App.tsx';

const DetachedLogsPage = lazy(() =>
  import('./pages/DetachedLogsPage.tsx').then(m => ({ default: m.DetachedLogsPage }))
);
const DetachedSidebarPage = lazy(() =>
  import('./pages/DetachedSidebarPage.tsx').then(m => ({ default: m.DetachedSidebarPage }))
);
const DetachedEditorPage = lazy(() =>
  import('./pages/DetachedEditorPage.tsx').then(m => ({ default: m.DetachedEditorPage }))
);
const DetachedRegistryPage = lazy(() =>
  import('./pages/DetachedRegistryPage.tsx').then(m => ({ default: m.DetachedRegistryPage }))
);
const DetachedWorkflowPage = lazy(() =>
  import('./pages/DetachedWorkflowPage.tsx').then(m => ({ default: m.DetachedWorkflowPage }))
);
const DetachedMetricsPage = lazy(() =>
  import('./pages/DetachedMetricsPage.tsx').then(m => ({ default: m.DetachedMetricsPage }))
);
const DetachedVaultPage = lazy(() =>
  import('./pages/DetachedVaultPage.tsx').then(m => ({ default: m.DetachedVaultPage }))
);

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Suspense fallback={<div className="h-screen w-screen" style={{ background: '#08080a' }} />}>
        <Routes>
          <Route path="/" element={<App />} />
          <Route path="/logs" element={<DetachedLogsPage />} />
          <Route path="/sidebar" element={<DetachedSidebarPage />} />
          <Route path="/editor" element={<DetachedEditorPage />} />
          <Route path="/registry" element={<DetachedRegistryPage />} />
          <Route path="/workflow" element={<DetachedWorkflowPage />} />
          <Route path="/metrics" element={<DetachedMetricsPage />} />
          <Route path="/vault" element={<DetachedVaultPage />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  </StrictMode>
);
