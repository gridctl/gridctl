import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import './index.css';
import App from './App.tsx';
import { DetachedLogsPage } from './pages/DetachedLogsPage.tsx';
import { DetachedSidebarPage } from './pages/DetachedSidebarPage.tsx';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<App />} />
        <Route path="/logs" element={<DetachedLogsPage />} />
        <Route path="/sidebar" element={<DetachedSidebarPage />} />
      </Routes>
    </BrowserRouter>
  </StrictMode>
);
