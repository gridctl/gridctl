import assert from 'node:assert/strict';
import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';
import { createServer } from 'vite';

// Run from the repository root with the externally installed Playwright module.
// This fixture exercises actual components in Chromium, without live mutations.
const { chromium } = await import(pathToFileURL(resolve(process.argv[2])).href);
const server = await createServer({
  root: resolve('web'),
  server: { host: '127.0.0.1', port: 0, strictPort: true },
  plugins: [{ name: 'execution-browser-fixture', configureServer(vite) {
    vite.middlewares.use((req, res, next) => {
      if (req.url !== '/__execution') return next();
      res.setHeader('Content-Type', 'text/html');
      void vite.transformIndexHtml('/__execution', `<html><body><div id="root"></div><script type="module">
        import React from 'react';
        import { createRoot } from 'react-dom/client';
        import { ExecutionFields } from '/src/components/wizard/steps/ExecutionFields.tsx';
        import { ExecutionDetails } from '/src/components/sidebar/ExecutionDetails.tsx';
        import { buildYAML, parseYAMLToForm } from '/src/lib/yaml-builder.ts';
        import '/src/index.css';
        function App() {
          const [data, setData] = React.useState({name:'fixture',serverType:'container',image:'alpine',transport:'stdio',execution:{mode:'hardened',uid:1000,gid:1000,read_only:false,drop_capabilities:[],mounts:[]}});
          const [idle, setIdle] = React.useState(false);
          const [missingControls, setMissingControls] = React.useState(false);
          const yaml = buildYAML({type:'mcp-server',data});
          const replica = {replicaId:0,healthy:true,execution:{mode:'hardened',revision:'fixture-revision',instance:'fixture-instance',outcome:'mismatch',eligible:false,runtime:'fixture',daemon_rootless:'unknown',user_namespace:'unknown',observed_at:'2026-09-12T00:00:00Z',controls:[{field:'memory_bytes',requested:'268435456',observed:'unknown',outcome:'mismatch',source:'fixture evidence'}]}};
          if (missingControls) replica.execution.controls = null;
          return React.createElement(React.Fragment, {},
            React.createElement(ExecutionFields, {data,onChange:patch=>setData({...data,...patch})}),
            React.createElement('button',{onClick:()=>{const parsed=parseYAMLToForm(yaml,'mcp-server');if('error' in parsed)throw Error(parsed.error);setData(parsed.data)}},'Round trip YAML'),
            React.createElement('pre',{'aria-label':'Authoritative proposed YAML'},yaml),
            React.createElement('button',{onClick:()=>setIdle(true)},'Idle to zero'),
            React.createElement('button',{onClick:()=>setMissingControls(true)},'Evidence unavailable'),
            React.createElement(ExecutionDetails,{server:{name:'fixture',execution:{mode:'hardened',revision:'fixture-revision',outcome:'pending',eligible:false},replicas:idle?[]:[replica]}})
          );
        }
        createRoot(document.getElementById('root')).render(React.createElement(App));
      </script></body></html>`).then(html => res.end(html));
    });
  } }],
});
let browser;
try {
  await server.listen();
  browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
  const failures = [];
  page.on('pageerror', error => failures.push(error.message));
  await page.goto(`http://127.0.0.1:${server.httpServer.address().port}/__execution`);
  const configuration = page.getByText('Execution', { exact: true });
  await configuration.focus();
  await page.keyboard.press('Enter');
  assert.equal(await configuration.evaluate(node => node.parentElement.open), true);
  await page.getByLabel('UID (nonzero numeric)').fill('1001');
  await page.getByRole('button', { name: 'Round trip YAML' }).click();
  const yaml = await page.getByLabel('Authoritative proposed YAML').textContent();
  assert.match(yaml, /uid: 1001/);
  assert.match(yaml, /read_only: false/);
  assert.match(yaml, /drop_capabilities: \[\]/);
  assert.match(yaml, /mounts: \[\]/);
  const evidence = page.getByText('Execution evidence', { exact: true });
  await evidence.focus();
  await page.keyboard.press('Enter');
  assert.equal(await evidence.evaluate(node => node.parentElement.open), true);
  assert.equal(await page.getByText(/MCP healthy; execution mismatch/).isVisible(), true);
  assert.equal(await page.getByRole('table', { name: 'Requested controls and evidence' }).isVisible(), true);
  await page.getByRole('button', { name: 'Evidence unavailable' }).click();
  assert.equal(await page.getByText('No per-control evidence available.').isVisible(), true);
  assert.equal(await page.getByText(/MCP healthy; execution mismatch/).isVisible(), true);
  assert.equal(await page.getByText(/0 of 1 active replicas/).isVisible(), true);
  await page.getByRole('button', { name: 'Idle to zero' }).click();
  assert.equal(await page.getByText(/No current active execution evidence/).isVisible(), true);
  assert.equal(await page.getByRole('table').count(), 0);
  assert.deepEqual(failures, []);
  console.log('Execution browser acceptance passed: keyboard disclosure, narrow layout, YAML presence preservation, health/evidence distinction, unavailable controls, and idle evidence removal.');
} finally {
  await browser?.close();
  await server.close();
}
