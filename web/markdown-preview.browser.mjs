import { createServer } from 'node:http';
import { copyFile, mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises';
import { extname, join, normalize, resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

// Manual acceptance for the incremental markdown preview. Not a CI gate.
// Run from the repository root:
//   node web/markdown-preview.browser.mjs <path-to-playwright-or-playwright-core>
// Uses the system Chrome channel. Measures a production build of the three
// editors: 2,000-line document, 35 keys at 33 ms intervals at the top,
// preview visible and hidden, CPU throttle 1x and 4x.
//
// MD_PREVIEW_ONLY=skill,canonical,fragment and MD_PREVIEW_RATES=1,4 narrow a run.

const playwrightPath = process.argv[2];
if (!playwrightPath) {
  console.error('usage: node web/markdown-preview.browser.mjs <playwright-module>');
  process.exit(1);
}

const KEYS = 'abcdefghijklmnopqrstuvwxyz012345678';
const root = resolve('web');
const outDir = '/tmp/opencode/md-preview-dist';
const fixtureHtml = join(root, 'markdown-preview.fixture.html');
const fixtureEntry = join(root, 'markdown-preview.fixture.tsx');
const editors = [
  { id: 'skill', label: 'Skill instructions' },
  { id: 'canonical', label: 'Canonical global context' },
  { id: 'fragment', label: 'Fragment 00-default.md' },
];
const only = new Set((process.env.MD_PREVIEW_ONLY || 'skill,canonical,fragment').split(','));
const rates = (process.env.MD_PREVIEW_RATES || '1,4').split(',').map((n) => Number(n));

const fence = '```';
const fixtureSource = `import React from 'react';
import { createRoot } from 'react-dom/client';
import { SkillEditor } from './src/components/registry/SkillEditor.tsx';
import { GlobalContextDialog } from './src/components/context/GlobalContextDialog.tsx';
import './src/index.css';

function buildDocument() {
  const chunks = [];
  let lines = 0;
  let n = 1;
  const fence = ${JSON.stringify(fence)};
  while (lines < 2000) {
    const chunk = [
      '## Section ' + n,
      '',
      'Paragraph ' + n + ' with **bold**, *italic*, and \`code\`.',
      '',
      '- first item ' + n,
      '- second item ' + n,
      '',
      fence + 'python',
      'def section_' + n + '(value):',
      '    return value + 1',
      fence,
      '',
    ];
    chunks.push(chunk.join('\\n'));
    lines += chunk.length;
    n += 1;
  }
  return chunks.join('\\n');
}

const doc = buildDocument();
const editor = new URLSearchParams(location.search).get('editor') || 'skill';
window.__fixture = { lines: doc.split('\\n').length, editor };
window.__fetches = [];

function json(body, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

window.fetch = async (input, init) => {
  const url = String(input);
  window.__fetches.push(((init && init.method) || 'GET') + ' ' + url);
  if (url.includes('/api/context/fragments')) {
    return json({
      active: true,
      fragments: [{
        name: '00-default.md',
        content: doc,
        bytes: doc.length,
        position: 0,
      }],
    });
  }
  if (url.includes('/api/context')) {
    return json({
      canonical: { path: '/fixture/AGENTS.md', exists: true, content: doc },
      fragments_active: editor === 'fragment',
      needs_sync: false,
      clients: [],
    });
  }
  if (url.includes('/validate')) {
    return json({ valid: true, errors: [], warnings: [] });
  }
  return json({});
};

const rootEl = document.getElementById('root');
if (editor === 'skill') {
  createRoot(rootEl).render(
    React.createElement(SkillEditor, {
      isOpen: true,
      onClose: () => {},
      onSaved: () => {},
      size: 'full',
      skill: {
        name: 'fixture-skill',
        description: 'Fixture skill for preview timing',
        body: doc,
        state: 'active',
        fileCount: 0,
      },
    }),
  );
} else {
  createRoot(rootEl).render(
    React.createElement(GlobalContextDialog, { isOpen: true, onClose: () => {} }),
  );
}
`;

const html = `<!doctype html>
<html>
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Markdown preview timing</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/markdown-preview.fixture.tsx"></script>
  </body>
</html>
`;

function p95(values) {
  if (values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const index = Math.min(sorted.length - 1, Math.ceil(sorted.length * 0.95) - 1);
  return sorted[index];
}

function round(ms) {
  return Math.round(ms * 10) / 10;
}

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.json': 'application/json',
  '.woff2': 'font/woff2',
  '.map': 'application/json',
};

function serve(dir) {
  return new Promise((resolveServer) => {
    const server = createServer(async (req, res) => {
      const url = new URL(req.url || '/', 'http://127.0.0.1');
      const rel = decodeURIComponent(url.pathname);
      const path = normalize(join(dir, rel === '/' ? 'index.html' : rel));
      if (!path.startsWith(dir)) {
        res.writeHead(403);
        res.end();
        return;
      }
      try {
        const body = await readFile(path);
        res.writeHead(200, { 'Content-Type': MIME[extname(path)] || 'application/octet-stream' });
        res.end(body);
      } catch {
        res.writeHead(404);
        res.end('not found');
      }
    });
    server.listen(0, '127.0.0.1', () => resolveServer(server));
  });
}

async function buildFixture() {
  await mkdir(outDir, { recursive: true });
  await writeFile(fixtureEntry, fixtureSource);
  await writeFile(fixtureHtml, html);
  const viteEntry = resolve('web/node_modules/vite/dist/node/index.js');
  const { build } = await import(pathToFileURL(viteEntry).href);
  await build({
    root,
    configFile: resolve('web/vite.config.ts'),
    logLevel: 'warn',
    build: {
      outDir,
      emptyOutDir: true,
      rolldownOptions: { input: fixtureHtml },
    },
  });
  const names = await readdir(outDir);
  const built = names.find((name) => name.endsWith('.html'));
  if (!built) throw new Error('production build produced no html');
  if (built !== 'index.html') await copyFile(join(outDir, built), join(outDir, 'index.html'));
}

async function measure(page, origin, editor, rate, preview) {
  const failures = [];
  page.removeAllListeners('pageerror');
  page.on('pageerror', (error) => failures.push(error.message));
  await page.goto(`${origin}/?editor=${editor.id}`, { waitUntil: 'networkidle' });
  await page.waitForFunction(
    (label) => {
      const ta = document.querySelector(`textarea[aria-label="${label}"]`);
      return Boolean(ta && !ta.readOnly && ta.value.includes('Section 1'));
    },
    editor.label,
    { timeout: 60000 },
  );
  if (preview) {
    await page.waitForFunction(
      () => document.querySelectorAll('.skill-md [data-md-block]').length > 50,
      { timeout: 60000 },
    );
    const box = await page.locator('.skill-md').boundingBox();
    if (!box || box.height < 20) throw new Error(`${editor.id} preview is not visible`);
    const margins = await page.evaluate(() => {
      const root = document.querySelector('.skill-md');
      const first = root.querySelector(':scope > [data-md-block]:first-child > :first-child');
      const last = root.querySelector(':scope > [data-md-block]:last-child > :last-child');
      return {
        firstTop: first ? getComputedStyle(first).marginTop : null,
        lastBottom: last ? getComputedStyle(last).marginBottom : null,
        firstTag: first ? first.tagName : null,
        lastTag: last ? last.tagName : null,
      };
    });
    if (margins.firstTop !== '0px' || margins.lastBottom !== '0px') {
      throw new Error(`${editor.id} preview margins ${JSON.stringify(margins)}`);
    }
  } else {
    await page.getByRole('button', { name: 'Hide preview' }).click();
    await page.waitForFunction(() => document.querySelector('.skill-md') == null);
  }

  const client = await page.context().newCDPSession(page);
  await client.send('Emulation.setCPUThrottlingRate', { rate });
  await page.waitForTimeout(200);

  const field = page.locator(`textarea[aria-label="${editor.label}"]`);
  await field.click();
  await field.evaluate((el) => {
    el.setSelectionRange(0, 0);
  });
  await page.evaluate(() => {
    window.__tasks = [];
    window.__delays = [];
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        window.__tasks.push({ start: entry.startTime, duration: entry.duration });
      }
    });
    observer.observe({ type: 'longtask', buffered: false });
    const ta = document.querySelector('textarea');
    ta.addEventListener(
      'keydown',
      (event) => {
        window.__delays.push(performance.now() - event.timeStamp);
      },
      true,
    );
    window.__typeStart = performance.now();
  });

  const typingStart = Date.now();
  await page.keyboard.type(KEYS, { delay: 33 });
  const typingMs = Date.now() - typingStart;
  const typeEnd = await page.evaluate(() => performance.now());

  const settleStart = Date.now();
  if (preview) {
    await page.waitForFunction(
      (prefix) => {
        const previewEl = document.querySelector('.skill-md');
        return Boolean(previewEl && previewEl.textContent && previewEl.textContent.includes(prefix));
      },
      KEYS.slice(0, 8),
      { timeout: 30000 },
    );
  }
  await page.evaluate(
    () =>
      new Promise((done) => {
        requestAnimationFrame(() => requestAnimationFrame(() => done()));
      }),
  );
  const settleMs = Date.now() - settleStart;

  const sample = await page.evaluate(() => ({
    tasks: window.__tasks,
    delays: window.__delays,
    typeStart: window.__typeStart,
    valueStart: document.querySelector('textarea').value.slice(0, 40),
    blocks: document.querySelectorAll('[data-md-block]').length,
    lines: window.__fixture.lines,
    fetches: window.__fetches,
  }));
  if (!sample.valueStart.startsWith(KEYS.slice(0, 8))) {
    throw new Error(`${editor.id} keys were not inserted at the top: ${sample.valueStart}`);
  }
  if (failures.length) throw new Error(failures.join('\n'));

  const during = sample.tasks.filter((task) => task.start + task.duration >= sample.typeStart && task.start <= typeEnd);
  const max = during.reduce((peak, task) => Math.max(peak, task.duration), 0);
  return {
    editor: editor.id,
    rate,
    preview,
    longTasks: during.length,
    longTaskMaxMs: round(max),
    typingMs: round(typingMs),
    settleMs: round(settleMs),
    inputDelayP95Ms: sample.delays.length ? round(p95(sample.delays)) : null,
    inputEvents: sample.delays.length,
    blocks: sample.blocks,
    lines: sample.lines,
  };
}

let server;
try {
  await buildFixture();
  server = await serve(outDir);
  const imported = await import(pathToFileURL(resolve(playwrightPath)).href);
  const chromium = imported.chromium || imported.default?.chromium;
  if (!chromium) throw new Error('playwright module has no chromium export');
  const browser = await chromium.launch({ headless: true, channel: 'chrome' });
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  page.setDefaultTimeout(120000);
  // The skill editor persists preview visibility. A hidden-baseline run must
  // not hide the preview for the next navigation.
  await page.addInitScript(() => {
    localStorage.clear();
  });
  await page.route('https://fonts.googleapis.com/**', (route) => route.abort());
  await page.route('https://fonts.gstatic.com/**', (route) => route.abort());
  const origin = `http://127.0.0.1:${server.address().port}`;
  const rows = [];
  for (const editor of editors) {
    if (!only.has(editor.id)) continue;
    for (const rate of rates) {
      for (const preview of [true, false]) {
        const row = await measure(page, origin, editor, rate, preview);
        rows.push(row);
        console.log(JSON.stringify(row));
      }
    }
  }
  await browser.close();
  console.log('MARKDOWN_PREVIEW_TIMING');
  console.log(JSON.stringify(rows, null, 2));
} finally {
  await new Promise((done) => {
    if (!server) done();
    else server.close(() => done());
  });
  await rm(fixtureHtml, { force: true });
  await rm(fixtureEntry, { force: true });
}
