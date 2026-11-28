import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createServer } from 'vite';

// Run from the repository root with the externally installed Playwright module.
// The real app shell loads at /stack and every /api/ response is fixture-owned:
// the shell's polls overwrite seeded stores, so the fixture is the data source.
// CHROMIUM_EXECUTABLE optionally names a browser binary when the Playwright
// browser cache does not match the installed Playwright version.
const { chromium } = await import(pathToFileURL(resolve(process.argv[2])).href);

const WIDTHS = [1024, 1100, 1280, 1366];
const HEIGHT = 800;
const OVERRIDE = '/home/fixture/stacks/platform-gateway/stack.override.yaml';

function server(name, extra = {}) {
  return { name, transport: 'http', endpoint: `http://127.0.0.1:9/${name}`, initialized: true, healthy: true, toolCount: 2, tools: ['read', 'write'], ...extra };
}
const finding = { code: 'hidden-instruction', severity: 'warn', confidence: 'high', field: 'description', message: 'Fixture finding' };
function serverPin(status, findings) {
  return {
    server_hash: 'h', pinned_at: '2026-11-01T00:00:00Z', last_verified_at: '2026-11-01T00:00:00Z', tool_count: 1, status,
    tools: { read: { hash: 'h', name: 'read', pinned_at: '2026-11-01T00:00:00Z', ...(findings ? { findings: [finding] } : {}) } },
  };
}
function documentPin(status, findings, hashKey) {
  return { [hashKey]: 'h', pinned_at: '2026-11-01T00:00:00Z', last_verified_at: '2026-11-01T00:00:00Z', status, ...(findings ? { findings: [finding] } : {}) };
}
function tokens(total, savings) {
  return {
    session: { input_tokens: total - 200, output_tokens: 200, total_tokens: total },
    per_server: {},
    format_savings: { original_tokens: 100, formatted_tokens: 100 - savings, saved_tokens: savings, savings_percent: savings },
  };
}
function health(override) {
  return {
    validation: { status: 'warnings', errorCount: 0, warningCount: 2 },
    drift: { status: 'in-sync' },
    dependencies: { status: 'resolved' },
    ...(override ? { override } : {}),
  };
}

const typicalStatus = {
  gateway: { name: 'fixture', version: 'test', tokenizer: 'api' },
  'mcp-servers': [server('github'), server('filesystem'), server('postgres')],
  resources: [{ name: 'cache', image: 'redis', status: 'running' }],
  sessions: 2,
  code_mode: 'on',
  token_usage: tokens(950, 0),
  stack_name: 'fixture',
  features: { fixture_flag: true },
  feature_details: [{ name: 'fixture_flag', stage: 'experimental', description: 'Fixture flag' }],
};

// Each fixture lists the alert badges it must render, so a missing badge
// fails the run instead of making the layout easier.
const FIXTURES = {
  typical: {
    status: typicalStatus,
    health: health(),
    pins: { github: serverPin('pinned', false), filesystem: serverPin('pinned', true) },
    skillPins: {},
    contextPins: {},
    limits: { configured: false, entries: [] },
    alerts: ['Spec: 2 warnings', 'Pins: OK', 'Findings: 1 server'],
  },
  worst: {
    status: {
      ...typicalStatus,
      'mcp-servers': [
        server('github'), server('filesystem'), server('postgres'),
        server('search', { healthy: false, healthError: 'connection refused' }),
        server('calendar', { external: true, authStatus: 'needs_auth' }),
      ],
      tool_discovery: 'deferred',
      gateway: { name: 'fixture', version: 'test', tokenizer: 'embedded' },
      token_usage: tokens(12345, 30),
    },
    health: health(OVERRIDE),
    pins: {
      github: serverPin('drift', true),
      filesystem: serverPin('pinned', true),
      postgres: serverPin('pinned', true),
    },
    skillPins: {
      review: documentPin('drift', true, 'skill_hash'),
      triage: documentPin('drift', true, 'skill_hash'),
    },
    contextPins: { 'commit-conventions': documentPin('drift', true, 'hash') },
    limits: { configured: true, entries: [{ kind: 'rate', scope: 'server', key: 'github', state: 'exceeded', rate: { calls_per_minute: 10, burst: 10 } }] },
    alerts: [
      'Spec: 2 warnings',
      'Pins: 4 drifted (2 skills, 1 fragment)',
      'Findings: 3 servers · 2 skills · 1 fragment',
      'Auth: 1 pending',
      '1 rate limit exceeded',
    ],
  },
  // Every alert badge idle: no spec health (stackless answer), no pins, no
  // pending authorization, and limits not configured.
  quiet: {
    status: typicalStatus,
    health: null,
    pins: {},
    skillPins: {},
    contextPins: {},
    limits: { configured: false, entries: [] },
    alerts: [],
  },
};

let fixture = FIXTURES.typical;
const unknownEndpoints = new Set();

function fixtureBody(path) {
  switch (path) {
    case '/api/status': return fixture.status;
    case '/api/tools': return { tools: [] };
    case '/api/tools/catalog': return { tools: [] };
    case '/api/clients': return [];
    case '/api/pins': return fixture.pins;
    case '/api/skill-pins': return fixture.skillPins;
    case '/api/context/pins': return fixture.contextPins;
    case '/api/stack/spec': return { path: '/home/fixture/stacks/platform-gateway/stack.yaml', content: 'name: fixture\n' };
    case '/api/stack/health': return fixture.health;
    case '/api/registry/status': return { totalSkills: 0, activeSkills: 0 };
    case '/api/registry/skills': return [];
    case '/api/skills/sources': return [];
    case '/api/telemetry/inventory': return [];
    case '/api/limits': return fixture.limits;
    case '/api/project/agents/status': return [];
    case '/api/project/wiring/status': return [];
    case '/api/hook-pins': return {};
    default:
      unknownEndpoints.add(path);
      return {};
  }
}

const server_ = await createServer({
  root: resolve('web'),
  logLevel: 'error',
  server: { host: '127.0.0.1', port: 0, strictPort: true },
  plugins: [{ name: 'shell-layout-fixture', configureServer(vite) {
    vite.middlewares.use((req, res, next) => {
      const path = (req.originalUrl || req.url || '').split('?')[0];
      if (!path.startsWith('/api/')) return next();
      res.setHeader('Content-Type', 'application/json');
      const body = fixtureBody(path);
      // Stackless gateways answer spec health with 503; the badge then renders nothing.
      if (body === null) { res.writeHead(503); res.end(JSON.stringify({ error: 'no stack loaded' })); return; }
      res.end(JSON.stringify(body));
    });
  } }],
});

// Runs in the page: every rectangle and overflow fact the assertions need.
function measure(alertLabels) {
  const rect = (el) => {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height };
  };
  const main = document.querySelector('main');
  const shell = main.parentElement;
  const header = document.querySelector('header');
  const bar = main.nextElementSibling;
  const buttons = [...bar.querySelectorAll('button')];
  const alerts = alertLabels.map((label) => {
    const button = buttons.find((b) => b.textContent.startsWith(label));
    if (!button) return { label, missing: true };
    const text = [...button.querySelectorAll('span.font-medium')].find((s) => s.textContent === label);
    return {
      label,
      rect: rect(button),
      natural: button.scrollWidth,
      icon: rect(button.querySelector('svg')),
      dot: rect(button.querySelector('span.rounded-full')),
      text: rect(text),
      truncated: text ? text.scrollWidth > text.clientWidth : null,
      title: button.getAttribute('title'),
    };
  });
  const alertGroup = alerts.find((a) => !a.missing)
    ? buttons.find((b) => b.textContent.startsWith(alertLabels[0])).parentElement
    : null;
  const counterGroup = bar.lastElementChild;
  const quietAlertGroup = counterGroup.previousElementSibling;
  const group = alertGroup ?? quietAlertGroup;
  const groupStyle = getComputedStyle(group);
  const after = getComputedStyle(group, '::after');
  const standaloneDividers = [...bar.children].filter((el) =>
    el.classList.contains('w-px') && getComputedStyle(el).display !== 'none' && el.getBoundingClientRect().width > 0,
  ).length;
  const groupDivider = groupStyle.display !== 'none' && after.content !== 'none' && after.content !== 'normal' && after.display !== 'none' ? 1 : 0;
  const connection = [...bar.querySelectorAll('span[title]')].find((s) => s.textContent === 'Connected');
  const counters = [...counterGroup.children]
    .filter((el) => el.getAttribute('aria-hidden') !== 'true')
    .map((el) => ({ text: el.textContent, rect: rect(el), button: el.tagName === 'BUTTON' }));
  return {
    inner: innerWidth,
    docScrollWidth: document.documentElement.scrollWidth,
    shellWidth: shell.getBoundingClientRect().width,
    shellScrollWidth: shell.scrollWidth,
    header: rect(header),
    headerControls: [...header.querySelectorAll('button, nav')].map((el) => ({
      name: el.getAttribute('aria-label') || el.tagName,
      rect: rect(el),
      displayed: el.getClientRects().length > 0,
    })),
    bar: rect(bar),
    connection: rect(connection),
    alerts,
    alertGroup: { display: groupStyle.display, rect: rect(group), children: group.childElementCount },
    dividers: standaloneDividers + groupDivider,
    counterGroup: { rect: rect(counterGroup), scrollTop: counterGroup.scrollTop, scrollLeft: counterGroup.scrollLeft },
    counters,
  };
}

const EPS = 0.5;
const inside = (r, box) => r.left >= box.left - EPS && r.right <= box.right + EPS && r.top >= box.top - EPS && r.bottom <= box.bottom + EPS;
const shown = (r) => r && r.width > 0 && r.height > 0;

function check(cond, message, data) {
  if (!cond) assert.fail(`${message}\n${JSON.stringify(data, null, 1)}`);
}

let browser;
const failures = [];
const summary = [];
try {
  await server_.listen();
  const origin = `http://127.0.0.1:${server_.httpServer.address().port}`;
  browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE || undefined });

  async function load(name, width) {
    fixture = FIXTURES[name];
    const page = await browser.newPage({ viewport: { width, height: HEIGHT } });
    page.on('pageerror', (error) => failures.push(`${name}@${width}: ${error.message}`));
    page.on('response', (response) => {
      const path = new URL(response.url()).pathname;
      const expected = path === '/api/stack/health' && fixture.health === null;
      if (path.startsWith('/api/') && response.status() >= 400 && !expected) {
        failures.push(`${name}@${width}: ${response.status()} ${path}`);
      }
    });
    await page.goto(`${origin}/stack`);
    await page.locator('main + footer').getByText('MCP servers').waitFor();
    await page.locator('main + footer').getByText(/^Updated /).waitFor();
    for (const label of fixture.alerts) {
      await page.locator('main + footer button').filter({ hasText: label }).first().waitFor();
    }
    // Let the slow poll and the canvas fit finish before measuring.
    await page.waitForFunction(() => document.querySelectorAll('.react-flow__node').length > 0);
    await stableNodes(page);
    return page;
  }

  async function stableNodes(page) {
    let previous = '';
    for (let tries = 0; tries < 40; tries++) {
      await page.waitForTimeout(150);
      const current = await page.evaluate(() => JSON.stringify(
        [...document.querySelectorAll('.react-flow__node')].map((n) => {
          const r = n.getBoundingClientRect();
          return [Math.round(r.left), Math.round(r.top), Math.round(r.right), Math.round(r.bottom)];
        }),
      ));
      if (current === previous) return;
      previous = current;
    }
  }

  for (const name of ['typical', 'worst']) {
    const alertLabels = FIXTURES[name].alerts;
    for (const width of WIDTHS) {
      const page = await load(name, width);
      const m = await page.evaluate(measure, alertLabels);
      const where = `${name} fixture at ${width} px`;

      check(m.alerts.every((a) => !a.missing), `${where}: an expected alert badge did not render`, m.alerts);
      check(m.header.width === m.inner, `${where}: header is ${m.header.width} px wide in a ${m.inner} px window`, m.alerts.map((a) => ({ label: a.label, natural: a.natural })));
      for (const control of m.headerControls) {
        check(control.rect.left >= -EPS && control.rect.right <= m.inner + EPS, `${where}: header control ${control.name} is outside the window`, control);
      }
      check(m.bar.width === m.inner, `${where}: status bar is ${m.bar.width} px wide`, m.bar);
      check(m.docScrollWidth === m.inner, `${where}: document scrollWidth ${m.docScrollWidth}`, m);
      check(m.shellScrollWidth === m.inner && m.shellWidth === m.inner, `${where}: shell root scrollWidth ${m.shellScrollWidth}`, m);
      check(m.bar.height === 32, `${where}: status bar is not one 32 px row`, m.bar);

      check(shown(m.connection) && inside(m.connection, m.bar), `${where}: connection label is outside the bar`, m.connection);
      for (const alert of m.alerts) {
        check(inside(alert.rect, m.bar), `${where}: ${alert.label} extends past the bar`, { bar: m.bar, alert });
        check(shown(alert.icon) && inside(alert.icon, m.bar), `${where}: ${alert.label} icon is not visible`, alert);
        check(shown(alert.dot) && inside(alert.dot, m.bar), `${where}: ${alert.label} status dot is not visible`, alert);
        // Findings keeps its explanatory title; its label is the button text.
        const expectedTitle = alert.label.startsWith('Findings:') ? alert.title : alert.label;
        check(alert.title === expectedTitle && alert.title, `${where}: ${alert.label} has no full-text title`, alert);
      }

      const visible = [];
      for (const counter of m.counters) {
        const isVisible = counter.rect.top < m.bar.bottom - EPS;
        if (isVisible) {
          visible.push(counter.text);
          check(inside(counter.rect, m.bar) && inside(counter.rect, m.counterGroup.rect), `${where}: counter "${counter.text}" is cut`, { counter, group: m.counterGroup, bar: m.bar });
        } else {
          check(counter.rect.top >= m.bar.bottom - EPS, `${where}: counter "${counter.text}" is partly visible`, { counter, bar: m.bar });
        }
      }
      check(m.counterGroup.scrollTop === 0 && m.counterGroup.scrollLeft === 0, `${where}: counter group is scrolled`, m.counterGroup);

      const truncated = m.alerts.filter((a) => a.truncated).map((a) => a.label);
      if (name === 'typical') {
        check(truncated.length === 0, `${where}: alert labels truncated`, m.alerts);
        if (width === 1366) {
          check(visible.length === m.counters.length, `${where}: not every status item is visible`, { visible, counters: m.counters });
          check(visible.at(-1).startsWith('Updated'), `${where}: Updated is not the last visible item`, visible);
        }
      }
      if (truncated.length > 0) {
        check(visible.length === 0, `${where}: alert labels truncated while counters are visible`, { truncated, visible });
      }
      // Counters drop only for lack of room: the first hidden item must not
      // fit in what is left of the first line (buttons carry -mx-1).
      const firstHidden = m.counters.findIndex((c) => c.rect.top >= m.bar.bottom - EPS);
      if (firstHidden >= 0) {
        const previous = m.counters.slice(0, firstHidden).at(-1);
        const room = m.counterGroup.rect.right - (previous ? previous.rect.right + 20 - (previous.button ? 4 : 0) : m.counterGroup.rect.left);
        const need = m.counters[firstHidden].rect.width - (m.counters[firstHidden].button ? 8 : 0);
        check(need > room - EPS, `${where}: "${m.counters[firstHidden].text}" hidden with ${room} px free`, { need, room, counters: m.counters });
      }

      if (width === 1024) {
        const canvas = await page.evaluate(() => {
          const pane = document.querySelector('.react-flow').getBoundingClientRect();
          const nodes = [...document.querySelectorAll('.react-flow__node')].map((n) => {
            const r = n.getBoundingClientRect();
            return { id: n.dataset.id, left: r.left, right: r.right, top: r.top, bottom: r.bottom };
          });
          return { pane: { left: pane.left, right: pane.right, top: pane.top, bottom: pane.bottom }, nodes };
        });
        check(canvas.pane.right <= m.inner + EPS, `${where}: canvas pane ends past the window`, canvas.pane);
        for (const node of canvas.nodes) {
          check(inside(node, canvas.pane), `${where}: node ${node.id} is framed outside the canvas`, canvas);
        }
      }

      if (name === 'worst' && width === 1024) {
        const hidden = m.counters.filter((c) => c.button && c.rect.top >= m.bar.bottom - EPS);
        check(hidden.length > 0, `${where}: no counter button is hidden by wrapping`, m.counters);
        const before = { connection: m.connection, first: m.alerts[0].rect };
        await page.locator('main + footer').getByRole('button', { name: /experimental flag|Open Metrics workspace/ })
          .filter({ hasNotText: 'rate limit' }).first().evaluate((el) => el.focus());
        const focused = await page.evaluate(() => document.activeElement?.getAttribute('aria-label'));
        check(/experimental flag|Open Metrics workspace/.test(focused ?? ''), `${where}: focus did not land on a hidden counter`, focused);
        const after = await page.evaluate(measure, alertLabels);
        check(after.counterGroup.scrollTop === 0 && after.counterGroup.scrollLeft === 0, `${where}: focusing a hidden counter scrolled the group`, after.counterGroup);
        check(JSON.stringify({ connection: after.connection, first: after.alerts[0].rect }) === JSON.stringify(before), `${where}: focusing a hidden counter changed the visible row`, { before, after: { connection: after.connection, first: after.alerts[0].rect } });
      }

      summary.push(`${name}@${width}: ${visible.length}/${m.counters.length} counters, ${truncated.length} truncated`);
      await page.close();
    }
  }

  const quiet = await load('quiet', 1024);
  const q = await quiet.evaluate(measure, []);
  check(q.alertGroup.display === 'none' && q.alertGroup.children === 0, 'quiet fixture: alert group renders a box', q.alertGroup);
  check(q.dividers === 1, `quiet fixture: ${q.dividers} dividers after the connection pill`, q);

  // The shell's column track must not grow with any child's minimum width,
  // not only the status bar's: add a wide grid child and remeasure.
  const wide = await quiet.evaluate(() => {
    const shell = document.querySelector('main').parentElement;
    const probe = document.createElement('div');
    probe.style.cssText = 'min-width: 3000px; grid-row: 2; grid-column: 1; visibility: hidden';
    shell.appendChild(probe);
    const result = {
      inner: innerWidth,
      header: document.querySelector('header').getBoundingClientRect().width,
      main: document.querySelector('main').getBoundingClientRect().width,
      shellScrollWidth: shell.scrollWidth,
    };
    probe.remove();
    return result;
  });
  check(wide.header === wide.inner && wide.main === wide.inner, `wide grid child: header is ${wide.header} px and main is ${wide.main} px in a ${wide.inner} px window`, wide);
  await quiet.close();

  // Header labels and target sizes. The typical fixture has no telemetry
  // block, so the Persistence pill reads "Persistence: Off".
  async function loadHeader(path, width) {
    fixture = FIXTURES.typical;
    const page = await browser.newPage({ viewport: { width, height: 900 } });
    page.on('pageerror', (error) => failures.push(`header ${path}@${width}: ${error.message}`));
    page.on('response', (response) => {
      const p = new URL(response.url()).pathname;
      if (p.startsWith('/api/') && response.status() >= 400) failures.push(`header ${path}@${width}: ${response.status()} ${p}`);
    });
    await page.goto(`${origin}${path}`);
    await page.locator('main + footer').getByText('MCP servers').waitFor();
    await page.locator('header button[title="Persistence: Off"]').waitFor();
    await page.evaluate(() => document.fonts.ready);
    await page.waitForTimeout(200);
    return page;
  }

  function measureHeader() {
    const rect = (el) => {
      const r = el.getBoundingClientRect();
      return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height };
    };
    // sr-only leaves a 1 px box; display:none leaves none.
    const textShown = (el) => Boolean(el) && el.getClientRects().length > 0 && el.getBoundingClientRect().width > 1;
    const header = document.querySelector('header');
    const nav = header.querySelector('nav[aria-label="Workspaces"]');
    const more = header.querySelector('button[aria-label="More actions"]');
    const cluster = more.closest('.shrink-0');
    const persistence = header.querySelector('button[title^="Persistence:"]');
    const create = header.querySelector('button[aria-label="Create new"]');
    return {
      inner: innerWidth,
      header: rect(header),
      nav: { rect: rect(nav), scrollWidth: nav.scrollWidth, clientWidth: nav.clientWidth },
      links: [...nav.querySelectorAll('a')].map((a) => ({
        name: a.getAttribute('aria-label'),
        rect: rect(a),
        labeled: textShown(a.querySelector('span')),
      })),
      controls: [...cluster.querySelectorAll('button')].map((b) => ({ name: b.getAttribute('aria-label') || b.getAttribute('title'), rect: rect(b) })),
      more: rect(more),
      persistence: rect(persistence),
      persistenceText: textShown(persistence.querySelector('span')),
      create: rect(create),
      createText: textShown(create.querySelector('span')),
    };
  }

  const near = (a, b) => Math.abs(a - b) <= 1;
  const big = (r) => r.width >= 32 - EPS && r.height >= 32 - EPS;
  const headerSummary = [];

  {
    const page = await loadHeader('/stack', 1280);
    const h = await page.evaluate(measureHeader);
    const where = 'header at 1280 px';
    check(h.links.length === 9 && h.links.every((l) => l.labeled), `${where}: not every workspace label is visible`, h.links);
    check(h.nav.scrollWidth <= h.nav.clientWidth, `${where}: the workspace nav overflows`, h.nav);
    for (const l of h.links) check(big(l.rect), `${where}: pill ${l.name} is under 32 by 32 px`, l);
    check(h.controls.length === 7, `${where}: expected seven cluster controls`, h.controls);
    for (const c of h.controls) check(big(c.rect), `${where}: control ${c.name} is under 32 by 32 px`, c);
    check(near(h.persistence.width, 38), `${where}: Persistence trigger is ${h.persistence.width} px wide`, h.persistence);
    check(near(h.create.width, 34) && near(h.create.height, 34), `${where}: Create new is ${h.create.width} by ${h.create.height} px`, h.create);
    check(h.header.right <= 1280 + EPS && h.more.right <= 1280 + EPS, `${where}: header or More actions ends past the window`, { header: h.header, more: h.more });
    headerSummary.push('1280 all labels');
    await page.close();
  }

  {
    const page = await loadHeader('/connections', 1100);
    const h = await page.evaluate(measureHeader);
    const where = 'header at 1100 px on /connections';
    const labeled = h.links.filter((l) => l.labeled).map((l) => l.name);
    check(labeled.length === 1 && labeled[0] === 'Connections', `${where}: labeled pills are ${labeled.join(', ')}`, h.links);
    for (const l of h.links.filter((x) => x.name !== 'Connections')) check(big(l.rect), `${where}: pill ${l.name} is under 32 by 32 px`, l);
    for (const c of h.controls) check(c.rect.left >= -EPS && c.rect.right <= h.inner + EPS, `${where}: control ${c.name} is outside the window`, c);
    const active = h.links.find((l) => l.name === 'Connections');
    check(active.rect.left >= h.nav.rect.left - EPS && active.rect.right <= h.nav.rect.right + EPS, `${where}: the Connections pill is not fully inside the nav`, { active, nav: h.nav });
    headerSummary.push('1100 active only');
    await page.close();
  }

  for (const width of [1024, 768, 390]) {
    const page = await loadHeader('/stack', width);
    const h = await page.evaluate(measureHeader);
    const where = `header at ${width} px`;
    check(h.header.width <= h.inner + EPS, `${where}: header is ${h.header.width} px wide`, h.header);
    check(h.controls.length === 7, `${where}: expected seven cluster controls`, h.controls);
    for (const c of h.controls) check(c.rect.left >= -EPS && c.rect.right <= h.inner + EPS, `${where}: control ${c.name} is outside the window`, c);
    headerSummary.push(`${width} fits`);
    await page.close();
  }

  for (const [width, labeledText] of [[1440, true], [1300, false]]) {
    const page = await loadHeader('/stack', width);
    const h = await page.evaluate(measureHeader);
    const where = `header at ${width} px`;
    check(h.links.every((l) => l.labeled), `${where}: not every workspace label is visible`, h.links);
    check(h.createText === labeledText, `${where}: Create new text visible is ${h.createText}`, h.create);
    check(h.persistenceText === labeledText, `${where}: Persistence text visible is ${h.persistenceText}`, h.persistence);
    headerSummary.push(`${width} ${labeledText ? 'labeled' : 'icon'} actions`);
    await page.close();
  }

  assert.deepEqual(failures, []);
  console.log(`Shell layout browser acceptance passed: ${summary.join('; ')}; quiet bar has one divider; header ${headerSummary.join(', ')}.${unknownEndpoints.size ? ` Unlisted endpoints answered empty: ${[...unknownEndpoints].join(', ')}.` : ''}`);
} finally {
  await browser?.close();
  await server_.close();
}
