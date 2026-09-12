import assert from 'node:assert/strict';
import { createServer as httpServer } from 'node:http';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';
import { createServer } from 'vite';

// Run from the repository root with a Playwright module path as the argument.
// All endpoints, credentials, contexts, and redirect targets are fixture-owned.
const { chromium } = await import(process.argv[2] ? pathToFileURL(resolve(process.argv[2])).href : 'playwright');
let arrivals = 0;
let secretArrivals = 0;
const target = httpServer((req, res) => {
  arrivals++;
  if (req.headers['x-fixture-credential']) secretArrivals++;
  res.end('destination');
});
await new Promise(r => target.listen(0, '127.0.0.1', r));
const targetURL = `http://127.0.0.1:${target.address().port}/destination`;
let redirect = '';
let rejection = false;
let holdNextStatus = false;
let pendingStatus;
let fixtureStatus = 200;
let malformed = false;
let holdStream = false;
let heldStream;
let streamCancelled = 0;
let rejectAfterSave = false;
let savedSkills = 0;
let received = [];
const server = await createServer({
  root: resolve('web'),
  server: { host: '127.0.0.1', port: 0, strictPort: true },
  plugins: [{
    name: 'isolated-auth-fixture',
    configureServer(vite) {
      vite.middlewares.use((req, res, next) => {
        if (req.url === '/destination') { arrivals++; if (req.headers['x-fixture-credential']) secretArrivals++; res.end('destination'); return; }
        if (req.url === '/__auth_test') {
          res.setHeader('Content-Type', 'text/html');
          void vite.transformIndexHtml('/__auth_test', `<html><body><div id="root"></div><script type="module">
            import React from 'react';
            import { createRoot } from 'react-dom/client';
            import { AuthBoundary } from '/src/components/auth/AuthBoundary.tsx';
            import { useAuthStore } from '/src/stores/useAuthStore.ts';
            import { useSSEShutdown } from '/src/hooks/useSSEShutdown.ts';
            import '/src/index.css';
            useAuthStore.setState({authRequired:true});
            function Content() { useSSEShutdown(() => { window.fixtureShutdown = true; }); return React.createElement('button', {}, 'Content'); }
            createRoot(document.getElementById('root')).render(React.createElement(AuthBoundary, {}, React.createElement(Content)));
          </script></body></html>`).then(html => res.end(html));
          return;
        }
        if (!req.url.startsWith('/api/') && req.url !== '/mcp' && req.url !== '/sse') return next();
        received.push({ path: req.url, bearer: !!req.headers.authorization?.startsWith('Bearer '), raw: !!req.headers.authorization && !req.headers.authorization.startsWith('Bearer '), custom: !!req.headers['x-fixture-credential'] });
        if (req.url === '/api/status' && holdNextStatus) { holdNextStatus = false; pendingStatus = res; return; }
        if (redirect) { res.writeHead(302, { Location: redirect }); res.end(); return; }
        if (rejection) { res.writeHead(401, { 'Gridctl-Auth-Rejected': '1' }); res.end(); return; }
        if (req.method === 'PUT' && req.url === '/api/registry/skills/fixture') {
          savedSkills++;
          res.setHeader('Content-Type', 'application/json');
          res.end(JSON.stringify({ name: 'fixture', description: 'Fixture skill', state: 'draft', body: 'Fixture instructions', fileCount: 0 }));
          if (rejectAfterSave) { rejectAfterSave = false; rejection = true; }
          return;
        }
        if (fixtureStatus !== 200) { res.writeHead(fixtureStatus); res.end(); return; }
        if (malformed) { res.end('<html>'); return; }
        if (req.url === '/sse') {
          res.writeHead(200, { 'Content-Type': 'text/event-stream' });
          if (holdStream) { heldStream = res; res.flushHeaders(); res.on('close', () => { if (!res.writableEnded) streamCancelled++; }); return; }
          res.end('event: endpoint\ndata: /mcp\n\n'); return;
        }
        res.setHeader('Content-Type', 'application/json');
        const body = req.url === '/api/status' ? { gateway: { name: 'fixture', version: 'test' }, 'mcp-servers': [] }
          : req.url.startsWith('/api/traces') ? { traces: [], total: 0, tracingEnabled: true, bufferSize: 0, bufferCapacity: 100 }
          : req.url.startsWith('/api/metrics/tokens') ? { range: '30m', interval: '1m', data_points: [], per_server: {} }
          : req.url.startsWith('/api/logs') ? { logs: [], total: 0, bufferCapacity: 100 }
          : req.url === '/api/registry/status' ? { totalSkills: 0, activeSkills: 0 }
          : req.url === '/api/registry/skills/fixture' ? { name: 'fixture', description: 'Fixture skill', state: 'draft', body: 'Fixture instructions', fileCount: 0 }
          : req.url === '/mcp' ? { jsonrpc: '2.0', id: 1, result: { tools: [] } }
          : req.url.includes('tools') ? { tools: [] }
          : req.url === '/api/reload' ? { success: true, message: 'unchanged' } : [];
        res.end(JSON.stringify(body));
      });
    },
  }],
});
let browser;
try {
  await server.listen();
  const origin = `http://127.0.0.1:${server.httpServer.address().port}`;
  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 320, height: 700 } });
  const page = await context.newPage();
  await page.goto(`${origin}/__auth_test`);
  await page.getByLabel('Credential', { exact: true }).waitFor();
  await page.getByLabel('Credential', { exact: true }).fill(await page.evaluate(() => crypto.randomUUID()));
  await page.getByRole('button', { name: 'Authenticate', exact: true }).focus();
  await page.keyboard.press('Tab');
  assert.equal(await page.evaluate(() => document.activeElement.getAttribute('aria-label')), 'Authentication mode');
  await page.getByRole('button', { name: 'Show credential' }).click();
  assert.equal(await page.getByLabel('Credential', { exact: true }).getAttribute('type'), 'text');
  await page.getByRole('button', { name: 'Hide credential' }).click();
  await page.getByLabel('Authentication mode').selectOption('api_key');
  await page.getByLabel('Credential header').fill('X-Fixture-Credential');
  await page.getByLabel('Credential', { exact: true }).press('Enter');
  await page.getByRole('dialog').waitFor({ state: 'hidden' });
  assert.equal(await page.evaluate(() => document.activeElement.textContent), 'Content');
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  for (const mode of ['bearer', 'api_key', 'custom']) {
    received = [];
    const result = await page.evaluate(async mode => {
      const { useAuthStore } = await import('/src/stores/useAuthStore.ts');
      const { verifyCredential, gatewayRequest } = await import('/src/lib/gatewayRequest.ts');
      const api = await import('/src/lib/api.ts');
      const candidate = { mode: mode === 'bearer' ? 'bearer' : 'api_key', header: mode === 'custom' ? 'X-Fixture-Credential' : 'Authorization', token: crypto.randomUUID() };
      await verifyCredential(candidate);
      useAuthStore.getState().commitAttempt(useAuthStore.getState().beginAttempt(), candidate);
      await api.fetchStatus();
      await api.triggerReload();
      await api.fetchClients();
      await api.mcpRequest('tools/list');
      await api.previewPack({ repo: 'https://example.invalid/fixture' });
      await api.updateStackTelemetry({ persist: { logs: true } });
      const stream = await gatewayRequest('/sse', { headers: { Accept: 'text/event-stream' } });
      await stream.text();
      return !window.fixtureShutdown;
    }, mode);
    assert.equal(result, true);
    assert.ok(received.length >= 5);
    assert.ok(received.every(r => mode === 'bearer' ? r.bearer : mode === 'custom' ? r.custom : r.raw));
  }
  for (const destination of ['/destination', targetURL]) {
    redirect = destination;
    for (const family of ['verify', 'read', 'mutation', 'mcp', 'pack', 'telemetry', 'stream']) {
      const kind = await page.evaluate(async family => {
        const { gatewayRequest, verifyCredential } = await import('/src/lib/gatewayRequest.ts');
        const api = await import('/src/lib/api.ts');
        try {
          if (family === 'verify') await verifyCredential({ mode: 'api_key', header: 'X-Fixture-Credential', token: crypto.randomUUID() });
          else if (family === 'read') await api.fetchStatus();
          else if (family === 'mutation') await api.triggerReload();
          else if (family === 'mcp') await api.mcpRequest('tools/list');
          else if (family === 'pack') await api.previewPack({ repo: 'https://example.invalid/fixture' });
          else if (family === 'telemetry') await api.updateStackTelemetry({ persist: { logs: true } });
          else await gatewayRequest('/sse');
        } catch (error) { return error.kind; }
      }, family);
      assert.equal(kind, 'redirect');
    }
    const beforeStream = received.filter(r => r.path === '/sse').length;
    await page.evaluate(async () => {
      const { useAuthStore } = await import('/src/stores/useAuthStore.ts');
      const state = useAuthStore.getState();
      state.commitAttempt(state.beginAttempt(), state.credential);
    });
    for (let tries = 0; received.filter(r => r.path === '/sse').length === beforeStream && tries < 100; tries++) await new Promise(r => setTimeout(r, 10));
    assert.ok(received.filter(r => r.path === '/sse').length > beforeStream);
    assert.equal(await page.evaluate(() => !!window.fixtureShutdown), false);
  }
  redirect = '';
  holdStream = true;
  await page.evaluate(async () => {
    const { useAuthStore } = await import('/src/stores/useAuthStore.ts');
    const state = useAuthStore.getState(); state.commitAttempt(state.beginAttempt(), state.credential);
  });
  for (let tries = 0; !heldStream && tries < 100; tries++) await new Promise(r => setTimeout(r, 10));
  assert.ok(heldStream);
  holdStream = false;
  await page.evaluate(async () => {
    const { useAuthStore } = await import('/src/stores/useAuthStore.ts');
    const state = useAuthStore.getState(); state.commitAttempt(state.beginAttempt(), state.credential);
  });
  for (let tries = 0; !streamCancelled && tries < 100; tries++) await new Promise(r => setTimeout(r, 10));
  assert.ok(streamCancelled > 0);
  heldStream.end();
  assert.equal(arrivals, 0);
  assert.equal(secretArrivals, 0);
  assert.equal(await page.evaluate(async destination => {
    const { gatewayRequest } = await import('/src/lib/gatewayRequest.ts');
    try { await gatewayRequest(destination); } catch (error) { return error.kind; }
  }, targetURL), 'origin');
  rejection = true;
  await page.evaluate(async () => {
    const { fetchStatus } = await import('/src/lib/api.ts');
    try { await fetchStatus(); } catch { /* Boundary observes rejection. */ }
  });
  await page.getByRole('dialog').waitFor();
  assert.equal(await page.evaluate(() => !!window.fixtureShutdown), false);
  const pausedCount = received.length;
  assert.equal(await page.evaluate(async () => {
    const { triggerReload } = await import('/src/lib/api.ts');
    try { await triggerReload(); } catch (error) { return error.kind; }
  }), 'stale');
  assert.equal(received.length, pausedCount);
  for (const route of ['/sidebar', '/editor?type=skill&name=fixture', '/library-window', '/metrics-window', '/logs-window', '/traces-window']) {
    rejection = true;
    const detached = await context.newPage();
    let pageErrors = 0;
    detached.on('pageerror', () => pageErrors++);
    await detached.goto(`${origin}${route}`);
    await detached.getByRole('dialog', { name: 'Authentication Required' }).waitFor();
    rejection = false;
    await detached.getByLabel('Credential', { exact: true }).fill(await detached.evaluate(() => crypto.randomUUID()));
    await detached.getByLabel('Credential', { exact: true }).press('Enter');
    await detached.getByRole('dialog', { name: 'Authentication Required' }).waitFor({ state: 'hidden' });
    await detached.waitForTimeout(50);
    assert.equal(pageErrors, 0);
    await detached.close();
  }
  rejection = false;
  const editor = await context.newPage();
  const editorErrors = [];
  editor.on('pageerror', error => editorErrors.push(error.name));
  await editor.addInitScript(() => { window.fixtureCloseCount = 0; window.close = () => { window.fixtureCloseCount++; }; });
  await editor.goto(`${origin}/editor?type=skill&name=fixture`);
  try {
    await editor.getByRole('button', { name: 'Save', exact: true }).waitFor();
  } catch {
    const state = await editor.evaluate(async () => {
      const { useAuthStore } = await import('/src/stores/useAuthStore.ts');
      return {
        authRequired: useAuthStore.getState().authRequired,
        loading: document.body.textContent.includes('Loading skill...'),
        retry: [...document.querySelectorAll('button')].some(button => button.textContent === 'Retry'),
      };
    });
    throw new Error(`Editor did not load: ${JSON.stringify({ ...state, editorErrors })}`);
  }
  rejectAfterSave = true;
  await editor.getByRole('button', { name: 'Save', exact: true }).click();
  await editor.getByRole('dialog', { name: 'Authentication Required' }).waitFor();
  assert.equal(savedSkills, 1);
  assert.equal(await editor.evaluate(() => window.fixtureCloseCount), 0);
  rejection = false;
  await editor.getByLabel('Credential', { exact: true }).fill(await editor.evaluate(() => crypto.randomUUID()));
  await editor.getByLabel('Credential', { exact: true }).press('Enter');
  await editor.getByRole('dialog', { name: 'Authentication Required' }).waitFor({ state: 'hidden' });
  assert.equal(savedSkills, 1);
  await editor.getByRole('button', { name: 'Save', exact: true }).click();
  await editor.waitForFunction(() => window.fixtureCloseCount === 1);
  assert.equal(savedSkills, 2);
  await editor.close();
  const memoryOnly = await context.newPage();
  await memoryOnly.addInitScript(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function(key, value) {
      if (key === 'gridctl-auth-credential') throw new DOMException('Storage unavailable', 'QuotaExceededError');
      return original.call(this, key, value);
    };
  });
  await memoryOnly.goto(`${origin}/__auth_test`);
  await memoryOnly.getByLabel('Credential', { exact: true }).fill(await memoryOnly.evaluate(() => crypto.randomUUID()));
  await memoryOnly.getByLabel('Credential', { exact: true }).press('Enter');
  await memoryOnly.getByRole('dialog').waitFor({ state: 'hidden' });
  await memoryOnly.getByRole('status').filter({ hasText: 'Verified for this window only' }).waitFor();
  const beforeInvalid = received.length;
  assert.equal(await memoryOnly.evaluate(async () => {
    const { verifyCredential } = await import('/src/lib/gatewayRequest.ts');
    for (const header of ['bad header', 'HOST', 'Cookie', 'Sec-Fixture', 'Proxy-Fixture', 'mCp-SeSsIoN-Id', 'content-TYPE']) {
      const token = crypto.randomUUID();
      try { await verifyCredential({ mode: 'api_key', header, token }); return false; }
      catch (error) { if (error.message.includes(token)) return false; }
    }
    try { await verifyCredential({ mode: 'api_key', header: 'X-Credential', token: crypto.randomUUID() + '\r\n' }); return false; }
    catch { return true; }
  }), true);
  assert.equal(received.length, beforeInvalid);
  for (const status of [200, 401]) {
    pendingStatus = undefined;
    holdNextStatus = true;
    await memoryOnly.evaluate(async () => {
      const { gatewayRequest } = await import('/src/lib/gatewayRequest.ts');
      window.pendingFixture = gatewayRequest('/api/status').then(() => 'unexpected success', error => error.kind);
    });
    for (let tries = 0; !pendingStatus && tries < 100; tries++) await new Promise(r => setTimeout(r, 10));
    assert.ok(pendingStatus);
    await memoryOnly.evaluate(async () => {
      const { useAuthStore } = await import('/src/stores/useAuthStore.ts');
      const { verifyCredential } = await import('/src/lib/gatewayRequest.ts');
      const candidate = { mode: 'bearer', header: 'Authorization', token: crypto.randomUUID() };
      await verifyCredential(candidate);
      useAuthStore.getState().commitAttempt(useAuthStore.getState().beginAttempt(), candidate);
    });
    pendingStatus.writeHead(status, { 'Gridctl-Auth-Rejected': '1', 'Content-Type': 'application/json' });
    pendingStatus.end('{}');
    assert.equal(await memoryOnly.evaluate(() => window.pendingFixture), 'stale');
    assert.equal(await memoryOnly.evaluate(async () => (await import('/src/stores/useAuthStore.ts')).useAuthStore.getState().authRequired), false);
  }
  for (const status of [401, 403, 500]) {
    fixtureStatus = status;
    const result = await memoryOnly.evaluate(async () => {
      const { fetchStatus } = await import('/src/lib/api.ts');
      const { useAuthStore } = await import('/src/stores/useAuthStore.ts');
      const prior = useAuthStore.getState().credential;
      try { await fetchStatus(); } catch { /* Assert preservation below. */ }
      return !useAuthStore.getState().authRequired && useAuthStore.getState().credential === prior;
    });
    assert.equal(result, true);
  }
  fixtureStatus = 200;
  malformed = true;
  assert.equal(await memoryOnly.evaluate(async () => {
    const { verifyCredential } = await import('/src/lib/gatewayRequest.ts');
    try { await verifyCredential({ mode: 'bearer', header: 'Authorization', token: crypto.randomUUID() }); } catch (error) { return error.kind; }
  }), 'malformed');
  malformed = false;
  await context.setOffline(true);
  assert.equal(await memoryOnly.evaluate(async () => {
    const { gatewayRequest } = await import('/src/lib/gatewayRequest.ts');
    try { await gatewayRequest('/api/status'); } catch (error) { return error.kind; }
  }), 'connection');
  await context.setOffline(false);
  assert.equal(await memoryOnly.evaluate(async () => {
    const { gatewayRequest } = await import('/src/lib/gatewayRequest.ts');
    const controller = new AbortController(); controller.abort();
    try { await gatewayRequest('/api/status', { signal: controller.signal }); } catch (error) { return error.kind; }
  }), 'abort');
  await memoryOnly.close();
  await context.close();
  console.log('Chromium: prompt keyboard/narrow layout, request modes, streaming negotiation, gateway rejection, and same/cross-origin redirect refusal passed.');
} finally {
  await browser?.close();
  await server.close();
  await new Promise(r => target.close(r));
}
