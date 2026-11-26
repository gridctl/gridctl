import { describe, expect, it } from 'vitest';
import type { Node } from '@xyflow/react';

import { chooseServerColumns, createButterflyEngine } from '../lib/graph/butterfly';
import { estimateFullCardHeight, getNodeDimensions } from '../lib/graph/utils';
import {
  createClientNodes,
  createGatewayNode,
  createMCPServerNodes,
  createResourceNodes,
  type ServerPinState,
} from '../lib/graph/nodes';
import { LAYOUT } from '../lib/constants';
import type { ClientStatus, MCPServerStatus, ResourceStatus } from '../types';

function server(name: string, overrides: Partial<MCPServerStatus> = {}): MCPServerStatus {
  return {
    name,
    transport: 'http',
    endpoint: `http://127.0.0.1:9000/${name}`,
    initialized: true,
    healthy: true,
    toolCount: 1,
    tools: ['read'],
    ...overrides,
  };
}

function servers(count: number): MCPServerStatus[] {
  return Array.from({ length: count }, (_, i) => server(`s${String(i).padStart(2, '0')}`));
}

const client: ClientStatus = { name: 'Client', slug: 'client', detected: true, linked: true, transport: 'http' };
const resource: ResourceStatus = { name: 'cache', image: 'redis:7', network: 'net', status: 'running' } as ResourceStatus;

interface GraphInput {
  mcpServers: MCPServerStatus[];
  clients?: ClientStatus[];
  resources?: ResourceStatus[];
  pins?: Record<string, ServerPinState> | null;
}

function graph({ mcpServers, clients = [client], resources = [], pins = null }: GraphInput): Node[] {
  return [
    createGatewayNode({ name: 'gw', version: '0' }, mcpServers, resources),
    ...createClientNodes(clients),
    ...createMCPServerNodes(mcpServers, pins),
    ...createResourceNodes(resources),
  ];
}

const engine = createButterflyEngine();

function layout(input: GraphInput, compact: boolean, preservedPositions?: Map<string, { x: number; y: number }>) {
  return engine.layout({ nodes: graph(input), edges: [] }, { compact, preservedPositions }).nodes;
}

const serverNodes = (nodes: Node[]) => nodes.filter((n) => n.type === 'mcpServer');

/** Server nodes grouped by x, columns left to right, cards top to bottom. */
function columnsOf(nodes: Node[]): Node[][] {
  const byX = new Map<number, Node[]>();
  for (const node of serverNodes(nodes)) {
    byX.set(node.position.x, [...(byX.get(node.position.x) ?? []), node]);
  }
  return [...byX.entries()]
    .sort(([a], [b]) => a - b)
    .map(([, column]) => column.sort((a, b) => a.position.y - b.position.y));
}

function overlaps(a: Node, b: Node, compact: boolean): boolean {
  const da = getNodeDimensions(a, compact);
  const db = getNodeDimensions(b, compact);
  return (
    a.position.x < b.position.x + db.width
    && a.position.x + da.width > b.position.x
    && a.position.y < b.position.y + db.height
    && a.position.y + da.height > b.position.y
  );
}

function expectNoOverlap(nodes: Node[], compact: boolean) {
  const cards = serverNodes(nodes);
  for (let i = 0; i < cards.length; i += 1) {
    for (let j = i + 1; j < cards.length; j += 1) {
      expect(overlaps(cards[i], cards[j], compact), `${cards[i].id} overlaps ${cards[j].id}`).toBe(false);
    }
  }
}

const pitchX = LAYOUT.NODE_WIDTH + LAYOUT.SERVER_COLUMN_GAP;

describe('chooseServerColumns', () => {
  it.each([
    { serverCount: 0, columns: 1 },
    { serverCount: 1, columns: 1 },
    { serverCount: 9, columns: 1 },
    { serverCount: 10, columns: 2 },
    { serverCount: 20, columns: 2 },
    { serverCount: 60, columns: 4 },
    { serverCount: 500, columns: LAYOUT.SERVER_MAX_COLUMNS },
  ])('compact with clients: $serverCount servers -> $columns', ({ serverCount, columns }) => {
    expect(chooseServerColumns({ serverCount, clientCount: 1, compact: true })).toBe(columns);
  });

  it('compact with no clients wraps at nine servers', () => {
    expect(chooseServerColumns({ serverCount: 8, compact: true })).toBe(1);
    expect(chooseServerColumns({ serverCount: 9, compact: true })).toBe(2);
  });

  it('full mode with clients wraps at four servers', () => {
    expect(chooseServerColumns({ serverCount: 3, clientCount: 1, compact: false })).toBe(1);
    expect(chooseServerColumns({ serverCount: 4, clientCount: 1, compact: false })).toBe(2);
  });

  it('never chooses more columns than servers', () => {
    for (let serverCount = 1; serverCount <= 80; serverCount += 1) {
      for (const compact of [true, false]) {
        const columns = chooseServerColumns({ serverCount, clientCount: 2, resourceCount: 1, compact });
        expect(columns).toBeLessThanOrEqual(Math.min(serverCount, LAYOUT.SERVER_MAX_COLUMNS));
        // Every chosen column holds at least one card.
        const rows = Math.ceil(serverCount / columns);
        expect(Math.ceil(serverCount / rows)).toBe(columns);
      }
    }
  });
});

describe('server grid layout, compact', () => {
  it('lays 20 servers into two columns of 10', () => {
    const nodes = layout({ mcpServers: servers(20) }, true);
    const columns = columnsOf(nodes);
    expect(columns.map((c) => c.length)).toEqual([10, 10]);
    expect(columns[1][0].position.x - columns[0][0].position.x).toBe(pitchX);
    expectNoOverlap(nodes, true);
  });

  it('lays 60 servers into four columns of 15', () => {
    const nodes = layout({ mcpServers: servers(60) }, true);
    const columns = columnsOf(nodes);
    expect(columns.map((c) => c.length)).toEqual([15, 15, 15, 15]);
    expectNoOverlap(nodes, true);
  });

  it('fills column-major with the first server at the top of column 0', () => {
    const input = servers(20);
    const nodes = layout({ mcpServers: input }, true);
    const columns = columnsOf(nodes);
    expect(columns[0][0].id).toBe(`mcp-${input[0].name}`);
    expect(columns.flat().map((n) => n.id)).toEqual(input.map((s) => `mcp-${s.name}`));
    // Columns are top-aligned.
    expect(columns[1][0].position.y).toBe(columns[0][0].position.y);
    // Node order still follows the input order.
    expect(serverNodes(nodes).map((n) => n.id)).toEqual(input.map((s) => `mcp-${s.name}`));
  });

  it('places resources right of the last server column', () => {
    const nodes = layout({ mcpServers: servers(60), resources: [resource] }, true);
    const lastColumnRight = Math.max(...serverNodes(nodes).map((n) => n.position.x + LAYOUT.NODE_WIDTH));
    const res = nodes.find((n) => n.type === 'resource')!;
    expect(res.position.x).toBeGreaterThan(lastColumnRight);
    expect(res.position.x).toBe(1200 - LAYOUT.NODE_WIDTH / 2 + 3 * pitchX);
  });

  it('keeps the single-column positions for five servers', () => {
    const nodes = layout({ mcpServers: servers(5), resources: [resource] }, true);
    const cards = serverNodes(nodes);
    const top = -(5 * 48 + 4 * 40) / 2 + 40;
    cards.forEach((card, i) => {
      expect(card.position).toEqual({ x: 672, y: top + i * 88 });
    });
    expect(nodes.find((n) => n.type === 'resource')!.position).toEqual({ x: 1072, y: -48 / 2 + 40 });
  });

  it('does not move clients or the gateway when servers wrap', () => {
    const few = layout({ mcpServers: servers(2) }, true);
    const many = layout({ mcpServers: servers(60) }, true);
    for (const id of ['gateway', 'client-client']) {
      expect(many.find((n) => n.id === id)!.position).toEqual(few.find((n) => n.id === id)!.position);
    }
  });

  it('keeps a dragged server and every other slot in a multi-column layout', () => {
    const input = { mcpServers: servers(20), resources: [resource] };
    const before = layout(input, true);
    const dragged = { x: 2400, y: -900 };
    const after = layout(input, true, new Map([['mcp-s03', dragged]]));
    for (const node of after) {
      if (node.id === 'mcp-s03') expect(node.position).toEqual(dragged);
      else expect(node.position).toEqual(before.find((n) => n.id === node.id)!.position);
    }
  });

  it('lays out zero and one server without throwing', () => {
    const none = layout({ mcpServers: [], clients: [] }, true);
    expect(none.map((n) => n.id)).toEqual(['gateway']);
    const one = layout({ mcpServers: servers(1), clients: [] }, true);
    expect(serverNodes(one)[0].position).toEqual({ x: 672, y: -24 + 40 });
  });
});

describe('server grid layout, full cards', () => {
  const mixed = (pins: Record<string, ServerPinState> | null = null) => ({
    mcpServers: [
      server('a-healthy'),
      server('b-error', { healthy: false, healthError: 'connection refused' }),
      server('c-auth', { external: true, authStatus: 'needs_auth', healthy: false }),
      server('d-drift'),
      server('e-noendpoint', { endpoint: undefined }),
      server('f-healthy'),
      server('g-healthy'),
      server('h-healthy'),
    ],
    resources: [resource],
    pins,
  });

  it('separates consecutive cards by 40 px of estimated height with no overlap', () => {
    const nodes = layout(mixed({ 'd-drift': { status: 'drift', tool_count: 2 } }), false);
    expectNoOverlap(nodes, false);
    const columns = columnsOf(nodes);
    expect(columns.length).toBeGreaterThan(1);
    for (const column of columns) {
      for (let i = 1; i < column.length; i += 1) {
        const above = column[i - 1];
        expect(column[i].position.y - (above.position.y + getNodeDimensions(above, false).height)).toBe(40);
      }
    }
  });

  it('chooses the same column count with or without drift', () => {
    const clean = columnsOf(layout(mixed(), false)).length;
    const drifted = columnsOf(layout(mixed({ 'a-healthy': { status: 'drift', tool_count: 1 }, 'f-healthy': { status: 'blocked', tool_count: 1 } }), false)).length;
    expect(drifted).toBe(clean);
  });

  it('moves only the cards below a card that gains a drift row', () => {
    const before = layout(mixed(), false);
    const columns = columnsOf(before);
    expect(columns.length).toBeGreaterThanOrEqual(2);
    const target = columns[0][1];
    const name = (target.data as { name: string }).name;
    const after = layout(mixed({ [name]: { status: 'drift', tool_count: 1 } }), false);
    const delta = getNodeDimensions(after.find((n) => n.id === target.id)!, false).height
      - getNodeDimensions(target, false).height;
    expect(delta).toBe(40);

    const position = (nodes: Node[], id: string) => nodes.find((n) => n.id === id)!.position;
    columns.forEach((column, c) => {
      column.forEach((node, row) => {
        const moved = c === 0 && row > 1 ? delta : 0;
        expect(position(after, node.id)).toEqual({ x: node.position.x, y: node.position.y + moved });
      });
    });
  });

  it('keeps the zone top fixed when card heights change', () => {
    const tops = [mixed(), mixed({ 'a-healthy': { status: 'drift', tool_count: 1 } })]
      .map((input) => Math.min(...serverNodes(layout(input, false)).map((n) => n.position.y)));
    expect(tops[1]).toBe(tops[0]);
  });
});

describe('estimateFullCardHeight', () => {
  const card = (s: MCPServerStatus, pins: Record<string, ServerPinState> | null = null) =>
    estimateFullCardHeight(createMCPServerNodes([s], pins)[0]);

  it('sums the rows a server card renders', () => {
    // base 75 + endpoint 43 + transport 27 + healthy 16 + status 29 + three 10 px gaps
    expect(card(server('a'))).toBe(75 + 43 + 27 + 16 + 29 + 30);
    // health error box replaces the healthy line
    expect(card(server('a', { healthy: false }))).toBe(75 + 43 + 27 + 30 + 29 + 30);
    // needs authorization suppresses the error box
    expect(card(server('a', { healthy: false, authStatus: 'needs_auth' }))).toBe(75 + 43 + 27 + 30 + 29 + 30);
    // drift adds a box
    expect(card(server('a'), { a: { status: 'drift', tool_count: 1 } })).toBe(75 + 43 + 27 + 16 + 30 + 29 + 40);
    // no endpoint or container drops the endpoint row
    expect(card(server('a', { endpoint: 'unknown' }))).toBe(75 + 27 + 16 + 29 + 20);
    // a disabled server shows neither health row, and a taller status row
    expect(card(server('a', { enabled: false, healthy: false }))).toBe(75 + 43 + 27 + 32 + 20);
  });

  it('sizes the transport row by whether its labels fit on one line', () => {
    const rest = 75 + 43 + 16 + 29 + 30;
    // a format chip and whitelist badge still fit beside the tool count
    expect(card(server('a', { outputFormat: 'csv', toolWhitelist: ['read'] }))).toBe(rest + 27);
    // tool, prompt, and MCP resource counts overflow, so a count label breaks
    expect(card(server('a', { toolCount: 12, promptCount: 3, mcpResourceCount: 4 }))).toBe(rest + 35);
    expect(card(server('a', {
      toolCount: 128, promptCount: 12, mcpResourceCount: 34, capabilities: { prompts: true, resources: true },
      outputFormat: 'toon', toolWhitelist: ['read'],
    }))).toBe(rest + 35);
    // a hyphenated transport chip breaks itself, the taller of the two
    const streamable = { transport: 'streamable-http' as MCPServerStatus['transport'] };
    expect(card(server('a', { ...streamable, toolCount: 2 }))).toBe(rest + 27);
    expect(card(server('a', { ...streamable, toolCount: 12, promptCount: 3 }))).toBe(rest + 41);
    // the json format renders no chip
    expect(card(server('a', { outputFormat: 'json', toolCount: 12, promptCount: 3 }))).toBe(rest + 27);
  });

  it('sums the rows a resource card renders', () => {
    expect(estimateFullCardHeight(createResourceNodes([resource])[0])).toBe(75 + 48 + 24 + 29 + 20);
  });

  it('falls back to the default height for nodes without card data', () => {
    const bare: Node = { id: 'x', type: 'mcpServer', position: { x: 0, y: 0 }, data: {} };
    expect(estimateFullCardHeight(bare)).toBe(LAYOUT.NODE_HEIGHT);
    expect(getNodeDimensions(bare, true).height).toBe(LAYOUT.NODE_HEIGHT_COMPACT);
  });
});
