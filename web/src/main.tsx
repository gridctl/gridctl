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

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Suspense>
        <Routes>
          <Route path="/" element={<App />} />
          <Route path="/logs" element={<DetachedLogsPage />} />
          <Route path="/sidebar" element={<DetachedSidebarPage />} />
          <Route path="/editor" element={<DetachedEditorPage />} />
          <Route path="/registry" element={<DetachedRegistryPage />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  </StrictMode>
);
