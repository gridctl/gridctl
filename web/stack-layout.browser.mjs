import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createServer } from 'vite';

// Run from the repository root with the externally installed Playwright module.
// The real app shell loads at /stack and every /api/ response is fixture-owned.
// It checks the Stack canvas server grid with 20 and 60 servers in compact and
// full card modes: no two server cards intersect, attention-state servers fill
// the first grid slots in bucket order, and in full mode each card's rendered
// height is within the layout's estimate. It prints the fit zoom per case.
// CHROMIUM_EXECUTABLE optionally names a browser binary when the Playwright
// browser cache does not match the installed Playwright version.
const { chromium } = await import(pathToFileURL(resolve(process.argv[2])).href);

const VIEWPORT = { width: 1440, height: 900 };
const SIZES = [20, 60];

const name = (i) => `s${String(i).padStart(2, '0')}`;
// Attention states sit mid-list so the sort, not the API order, puts them first.
const UNHEALTHY = new Set([3, 11]);
const BLOCKED = new Set([9]);
const NEEDS_AUTH = new Set([7]);
const DRIFT = new Set([5, 15]);
// Disabled and unhealthy, so it stays with the rest.
const DISABLED = new Set([13]);
// Bucket order, then API (name) order inside a bucket.
const EXPECTED_FIRST = [[...UNHEALTHY, ...BLOCKED].sort((a, b) => a - b), [...NEEDS_AUTH], [...DRIFT]]
  .flat()
  .map((i) => `mcp-${name(i)}`);

function server(i) {
  const base = {
    name: name(i), transport: 'http', endpoint: `http://127.0.0.1:9/${name(i)}`,
    initialized: true, healthy: true, toolCount: 2, tools: ['read', 'write'],
  };
  if (UNHEALTHY.has(i)) return { ...base, healthy: false, healthError: 'connection refused' };
  if (NEEDS_AUTH.has(i)) return { ...base, external: true, healthy: false, authStatus: 'needs_auth' };
  if (DISABLED.has(i)) return { ...base, enabled: false, healthy: false };
  // Fuller transport rows: the chip breaks at its hyphen, the count labels
  // break, and a format chip with a whitelist badge that stays on one line.
  if (i === 2) return { ...base, transport: 'streamable-http', toolCount: 12, promptCount: 3, mcpResourceCount: 4 };
  if (i === 4) {
    return {
      ...base, toolCount: 128, promptCount: 12, mcpResourceCount: 34, capabilities: { prompts: true, resources: true },
      outputFormat: 'toon', toolWhitelist: ['read'],
    };
  }
  if (i === 6) return { ...base, outputFormat: 'csv', toolWhitelist: ['read'] };
  // Odd servers are stdio containers, so both endpoint and container rows render.
  if (i % 2 === 1) return { ...base, transport: 'stdio', endpoint: undefined, containerId: `c0ffee${String(i).padStart(6, '0')}` };
  return base;
}

function serverPin(status) {
  return {
    server_hash: 'h', pinned_at: '2026-11-01T00:00:00Z', last_verified_at: '2026-11-01T00:00:00Z', tool_count: 1, status,
    tools: { read: { hash: 'h', name: 'read', pinned_at: '2026-11-01T00:00:00Z' } },
  };
}

let count = SIZES[0];
const unknownEndpoints = new Set();

function fixtureBody(path) {
  const servers = Array.from({ length: count }, (_, i) => server(i));
  switch (path) {
    case '/api/status': return {
      gateway: { name: 'fixture', version: 'test' },
      'mcp-servers': servers,
      resources: [{ name: 'cache', image: 'redis:7', network: 'fixture-net', status: 'running' }],
      sessions: 0,
      stack_name: 'fixture',
    };
    case '/api/pins': return Object.fromEntries(servers.map((s, i) => [s.name, serverPin(DRIFT.has(i) ? 'drift' : BLOCKED.has(i) ? 'blocked' : 'pinned')]));
    case '/api/clients': return [{ name: 'Fixture Client', slug: 'fixture', detected: true, linked: true, transport: 'http' }];
    case '/api/tools': return { tools: [] };
    case '/api/tools/catalog': return { tools: [] };
    case '/api/skill-pins': return {};
    case '/api/context/pins': return {};
    case '/api/hook-pins': return {};
    case '/api/stack/spec': return { path: '/home/fixture/stack.yaml', content: 'name: fixture\n' };
    case '/api/stack/health': return { validation: { status: 'valid', errorCount: 0, warningCount: 0 }, drift: { status: 'in-sync' }, dependencies: { status: 'resolved' } };
    case '/api/registry/status': return { totalSkills: 0, activeSkills: 0 };
    case '/api/registry/skills': return [];
    case '/api/skills/sources': return [];
    case '/api/telemetry/inventory': return [];
    case '/api/limits': return { configured: false, entries: [] };
    case '/api/project/agents/status': return [];
    case '/api/project/wiring/status': return [];
    default:
      unknownEndpoints.add(path);
      return {};
  }
}

const vite = await createServer({
  root: resolve('web'),
  logLevel: 'error',
  server: { host: '127.0.0.1', port: 0, strictPort: true },
  plugins: [{ name: 'stack-layout-fixture', configureServer(server) {
    server.middlewares.use((req, res, next) => {
      const path = (req.originalUrl || req.url || '').split('?')[0];
      if (!path.startsWith('/api/')) return next();
      res.setHeader('Content-Type', 'application/json');
      res.end(JSON.stringify(fixtureBody(path)));
    });
  } }],
});

// Runs in the page: server card rectangles, the viewport zoom, and the
// layout's own height estimate for each card.
async function measure() {
  const { useStackStore } = await import('/src/stores/useStackStore.ts');
  const { getNodeDimensions } = await import('/src/lib/graph/utils.ts');
  const transform = document.querySelector('.react-flow__viewport').style.transform;
  const zoom = Number(/scale\(([^)]+)\)/.exec(transform)[1]);
  const byId = new Map(useStackStore.getState().nodes.map((n) => [n.id, n]));
  const rects = (prefix) => [...document.querySelectorAll('.react-flow__node')]
    .filter((el) => el.dataset.id?.startsWith(prefix))
    .map((el) => {
      const r = el.getBoundingClientRect();
      const node = byId.get(el.dataset.id);
      return {
        id: el.dataset.id, left: r.left, top: r.top, right: r.right, bottom: r.bottom, height: r.height,
        estimate: node ? getNodeDimensions(node, false).height : null,
      };
    });
  return { zoom, cards: rects('mcp-'), resources: rects('resource-') };
}

function check(cond, message, data) {
  if (!cond) assert.fail(`${message}\n${JSON.stringify(data, null, 1)}`);
}

const intersects = (a, b) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;

let browser;
const failures = [];
const summary = [];
try {
  await vite.listen();
  const origin = `http://127.0.0.1:${vite.httpServer.address().port}`;
  browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE || undefined });

  async function stableNodes(page) {
    let previous = '';
    for (let tries = 0; tries < 60; tries++) {
      await page.waitForTimeout(150);
      const current = await page.evaluate(() => JSON.stringify([
        document.querySelector('.react-flow__viewport')?.style.transform,
        ...[...document.querySelectorAll('.react-flow__node')].map((n) => {
          const r = n.getBoundingClientRect();
          return [n.dataset.id, Math.round(r.left), Math.round(r.top), Math.round(r.right), Math.round(r.bottom)];
        }),
      ]));
      if (current === previous) return;
      previous = current;
    }
  }

  async function setMode(page, compact) {
    const want = compact ? 'Switch to full cards' : 'Switch to compact cards';
    const other = compact ? 'Switch to compact cards' : 'Switch to full cards';
    const button = page.getByRole('button', { name: other, exact: true });
    if (await button.count()) await button.click();
    await page.getByRole('button', { name: want, exact: true }).waitFor();
    await stableNodes(page);
  }

  for (const size of SIZES) {
    count = size;
    const page = await browser.newPage({ viewport: VIEWPORT });
    page.on('pageerror', (error) => failures.push(`${size}: ${error.message}`));
    // More than 24 servers opens the table view when no view is saved, so the
    // graph is chosen up front for the canvas checks.
    await page.addInitScript(() => {
      localStorage.setItem('gridctl-ui-storage', JSON.stringify({ state: { stackPrefs: { view: 'graph' } }, version: 1 }));
    });
    await page.goto(`${origin}/stack`);
    await page.waitForFunction((n) => document.querySelectorAll('.react-flow__node[data-id^="mcp-"]').length === n, size);
    // Pins arrive on the inventory poll; wait for the drift rows to reach node data.
    await page.waitForFunction(async (n) => {
      const { useStackStore } = await import('/src/stores/useStackStore.ts');
      return useStackStore.getState().nodes.filter((node) => node.data?.pinStatus === 'drift').length === n;
    }, DRIFT.size);
    await stableNodes(page);

    for (const compact of [true, false]) {
      await setMode(page, compact);
      const where = `${size} servers, ${compact ? 'compact' : 'full'}`;
      const m = await page.evaluate(measure);
      check(m.cards.length === size, `${where}: ${m.cards.length} server cards rendered`, m.cards.length);

      for (let i = 0; i < m.cards.length; i++) {
        for (let j = i + 1; j < m.cards.length; j++) {
          check(!intersects(m.cards[i], m.cards[j]), `${where}: ${m.cards[i].id} intersects ${m.cards[j].id}`, [m.cards[i], m.cards[j]]);
        }
      }

      const round = (v) => Math.round(v * 10) / 10;
      const order = [...m.cards].sort((a, b) => round(a.left) - round(b.left) || a.top - b.top).map((c) => c.id);
      check(
        JSON.stringify(order.slice(0, EXPECTED_FIRST.length)) === JSON.stringify(EXPECTED_FIRST),
        `${where}: attention servers do not fill the first grid slots`,
        { expected: EXPECTED_FIRST, actual: order.slice(0, EXPECTED_FIRST.length + 2) },
      );
      const columns = new Set(m.cards.map((c) => round(c.left))).size;

      let worst = 0;
      if (!compact) {
        check(m.resources.length === 1, `${where}: resource card missing`, m.resources);
        for (const card of [...m.cards, ...m.resources]) {
          const rendered = card.height / m.zoom;
          check(rendered <= card.estimate + 0.5, `${where}: ${card.id} renders ${rendered.toFixed(1)} px, above its ${card.estimate} px estimate`, card);
          check(card.estimate - rendered <= 24, `${where}: ${card.id} estimate ${card.estimate} px is more than 24 px above its ${rendered.toFixed(1)} px render`, card);
          worst = Math.max(worst, card.estimate - rendered);
        }
      }
      summary.push(`${where}: ${columns} columns, fit zoom ${m.zoom.toFixed(3)}${compact ? '' : `, estimate at most ${worst.toFixed(1)} px above render`}`);
    }

    // Reference: the same compact graph held to one server column, which is
    // the previous layout's compact geometry. Informational, not asserted.
    await setMode(page, true);
    const single = async (columns) => {
      const previous = await page.evaluate(async (max) => {
        const { LAYOUT } = await import('/src/lib/constants.ts');
        const { useStackStore } = await import('/src/stores/useStackStore.ts');
        const before = LAYOUT.SERVER_MAX_COLUMNS;
        LAYOUT.SERVER_MAX_COLUMNS = max;
        useStackStore.getState().resetLayout();
        return before;
      }, columns);
      await stableNodes(page);
      return { zoom: (await page.evaluate(measure)).zoom, previous };
    };
    const { zoom: reference, previous: maxColumns } = await single(1);
    const { zoom: restored } = await single(maxColumns);
    summary.push(`${size} servers, compact, one column: fit zoom ${reference.toFixed(3)} (grid is ${(restored / reference).toFixed(2)}x)`);
    await page.close();
  }

  assert.deepEqual(failures, []);
  console.log(`Stack layout browser acceptance passed at ${VIEWPORT.width}x${VIEWPORT.height}:\n  ${summary.join('\n  ')}${unknownEndpoints.size ? `\nUnlisted endpoints answered empty: ${[...unknownEndpoints].join(', ')}.` : ''}`);
} finally {
  await browser?.close();
  await vite.close();
}
