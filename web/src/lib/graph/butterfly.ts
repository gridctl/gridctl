/**
 * Butterfly layout engine - Hub-and-Spoke with 5 zones
 *
 * Implements a clean, logic-driven layout where the Gateway acts as
 * the central hub with zones arranged left-to-right:
 *
 * Zone 0 (Left):           Clients - linked LLM clients
 * Zone 1 (Center):         Gateway - central hub/router
 * Zone 2 (Right):          MCP Servers - providers/tools
 * Zone 3 (Far Right):      Resources - infrastructure/databases
 * Zone 5 (Far Far Right):  Skills - registered active skills
 */

import type { Node } from '@xyflow/react';
import type {
  LayoutEngine,
  LayoutInput,
  LayoutOutput,
  LayoutOptions,
  ZoneConfig,
} from './types';
import { ButterflyZone } from './types';
import { LAYOUT } from '../constants';
import { getNodeDimensions } from './utils';

/**
 * Default zone configuration
 * X positions create clear visual separation between zones
 * Zones: 0=CLIENTS, 1=GATEWAY, 2=SERVERS, 3=RESOURCES
 */
const DEFAULT_ZONE_CONFIGS: Record<ButterflyZone, ZoneConfig> = {
  [4]: { zone: 4, baseX: -400, nodeSpacing: 80 },       // CLIENTS (far left)
  [0]: { zone: 0, baseX: 0, nodeSpacing: 80 },          // AGENTS (left, reserved)
  [1]: { zone: 1, baseX: 400, nodeSpacing: 0 },         // GATEWAY (center)
  [2]: { zone: 2, baseX: 800, nodeSpacing: LAYOUT.SERVER_ROW_GAP, compactNodeSpacing: LAYOUT.SERVER_ROW_GAP }, // SERVERS (right)
  [3]: { zone: 3, baseX: 1200, nodeSpacing: LAYOUT.SERVER_ROW_GAP, compactNodeSpacing: LAYOUT.SERVER_ROW_GAP }, // RESOURCES (far right)
  [5]: { zone: 5, baseX: 1600, nodeSpacing: 80 }, // SKILLS (far far right)
};

// getNodeZone places linked clients in zone 0.
const CLIENTS_ZONE: ButterflyZone = 0;

/** Vertical gap between cards of a zone; compact halves it unless set. */
function zoneSpacing(config: ZoneConfig, compact: boolean): number {
  if (!compact) return config.nodeSpacing;
  return config.compactNodeSpacing ?? Math.round(config.nodeSpacing * 0.5);
}

/** Height of `count` cards of one height stacked with `gap` between them. */
function stackHeight(count: number, height: number, gap: number): number {
  return count > 0 ? count * height + (count - 1) * gap : 0;
}

/** Card height the server grid assumes for every card in a mode. */
function nominalCardHeight(compact: boolean): number {
  return compact ? LAYOUT.NODE_HEIGHT_COMPACT : LAYOUT.NODE_HEIGHT_FULL_NOMINAL;
}

/** Node counts per zone that decide the server column count. */
export interface ServerColumnInput {
  serverCount: number;
  clientCount?: number;
  resourceCount?: number;
  compact?: boolean;
}

/**
 * Choose how many columns the server zone wraps into.
 *
 * Each candidate count is scored by how large the whole graph's nominal
 * bounding box could be drawn in the reference box, capped at the fit
 * maxZoom; the best score wins and ties go to fewer columns. The result
 * depends only on zone counts and the card mode, never on card data, drags,
 * or the canvas size, so a click or a status change cannot reflow the grid.
 */
export function chooseServerColumns(
  input: ServerColumnInput,
  zoneConfigs: Record<ButterflyZone, ZoneConfig> = DEFAULT_ZONE_CONFIGS,
): number {
  const { serverCount, clientCount = 0, resourceCount = 0, compact = false } = input;
  const maxColumns = Math.min(LAYOUT.SERVER_MAX_COLUMNS, serverCount);
  if (maxColumns <= 1) return 1;

  const clients = zoneConfigs[CLIENTS_ZONE];
  const gateway = zoneConfigs[1];
  const servers = zoneConfigs[2];
  const resources = zoneConfigs[3];
  const nominal = nominalCardHeight(compact);
  const pitch = LAYOUT.NODE_WIDTH + LAYOUT.SERVER_COLUMN_GAP;

  const clientWidth = compact ? LAYOUT.CLIENT_WIDTH : LAYOUT.CLIENT_WIDTH_FULL;
  const clientHeight = compact ? LAYOUT.CLIENT_HEIGHT_COMPACT : LAYOUT.CLIENT_HEIGHT;
  const left = clientCount > 0
    ? clients.baseX - clientWidth / 2
    : gateway.baseX - LAYOUT.GATEWAY_WIDTH / 2;
  const fixedHeight = Math.max(
    LAYOUT.GATEWAY_HEIGHT,
    stackHeight(clientCount, clientHeight, zoneSpacing(clients, compact)),
    stackHeight(resourceCount, nominal, zoneSpacing(resources, compact)),
  );

  let best = 1;
  let bestScore = -Infinity;
  for (let columns = 1; columns <= maxColumns; columns += 1) {
    const offset = (columns - 1) * pitch;
    const right = resourceCount > 0
      ? resources.baseX + LAYOUT.NODE_WIDTH / 2 + offset
      : servers.baseX + LAYOUT.NODE_WIDTH / 2 + offset;
    const rows = Math.ceil(serverCount / columns);
    const height = Math.max(fixedHeight, stackHeight(rows, nominal, zoneSpacing(servers, compact)));
    const score = Math.min(
      LAYOUT.LAYOUT_REFERENCE_WIDTH / (right - left),
      LAYOUT.LAYOUT_REFERENCE_HEIGHT / height,
      LAYOUT.LAYOUT_MAX_SCORE,
    );
    if (score > bestScore) {
      best = columns;
      bestScore = score;
    }
  }
  return best;
}

/**
 * Determine which zone a node belongs to based on its type and data
 */
function getNodeZone(node: Node): ButterflyZone {
  const nodeData = node.data as Record<string, unknown>;
  const nodeType = nodeData?.type as string;

  switch (nodeType) {
    case 'client':
      return 0; // CLIENTS zone (far left)

    case 'gateway':
      return 1; // GATEWAY zone

    case 'mcp-server':
      return 2; // SERVERS zone

    case 'resource':
      return 3; // RESOURCES zone

    case 'skill':
    case 'skill-group':
      return ButterflyZone.SKILLS; // SKILLS zone

    default:
      return 2; // Default to SERVERS zone
  }
}

/**
 * Butterfly layout options
 */
export interface ButterflyLayoutOptions extends LayoutOptions {
  /** Custom zone configurations */
  zoneConfigs?: Partial<Record<ButterflyZone, ZoneConfig>>;
  /** Vertical margin from center */
  verticalMargin?: number;
}

/**
 * Butterfly layout engine implementation
 *
 * Places nodes in explicit zones with vertical centering within each zone.
 * The gateway acts as the central hub with agents on the left flowing
 * into it, and servers/resources on the right flowing out.
 */
export class ButterflyLayoutEngine implements LayoutEngine {
  readonly name = 'butterfly';
  private zoneConfigs: Record<ButterflyZone, ZoneConfig>;
  private verticalMargin: number;

  constructor(options: ButterflyLayoutOptions = {}) {
    this.zoneConfigs = {
      ...DEFAULT_ZONE_CONFIGS,
      ...options.zoneConfigs,
    };
    this.verticalMargin = options.verticalMargin ?? LAYOUT.MARGIN_Y;
  }

  layout(input: LayoutInput, options?: LayoutOptions): LayoutOutput {
    const { nodes, edges } = input;
    const preservedPositions = options?.preservedPositions;
    const compact = options?.compact ?? false;

    // Group nodes by zone
    const nodesByZone = new Map<ButterflyZone, Node[]>();

    for (const node of nodes) {
      const zone = getNodeZone(node);
      const existing = nodesByZone.get(zone) || [];
      existing.push(node);
      nodesByZone.set(zone, existing);
    }

    const serverCount = nodesByZone.get(2)?.length ?? 0;
    const columns = chooseServerColumns(
      {
        serverCount,
        clientCount: nodesByZone.get(CLIENTS_ZONE)?.length ?? 0,
        resourceCount: nodesByZone.get(3)?.length ?? 0,
        compact,
      },
      this.zoneConfigs,
    );
    // Resources stay one column, right of the last server column.
    const resourceShift = (columns - 1) * (LAYOUT.NODE_WIDTH + LAYOUT.SERVER_COLUMN_GAP);

    // Calculate positioned nodes for each zone
    const positionedNodes: Node[] = [];

    for (const [zone, zoneNodes] of nodesByZone) {
      const config = this.zoneConfigs[zone];
      const positioned = zone === 2
        ? this.layoutServerGrid(zoneNodes, config, columns, preservedPositions, compact)
        : this.layoutZone(
            zoneNodes,
            zone === 3 ? { ...config, baseX: config.baseX + resourceShift } : config,
            preservedPositions,
            compact,
          );
      positionedNodes.push(...positioned);
    }

    return { nodes: positionedNodes, edges };
  }

  /**
   * Layout nodes within a single zone
   * Positions nodes vertically centered around y=0
   */
  private layoutZone(
    nodes: Node[],
    config: ZoneConfig,
    preservedPositions?: Map<string, { x: number; y: number }>,
    compact = false
  ): Node[] {
    if (nodes.length === 0) return [];

    const spacing = zoneSpacing(config, compact);

    // Calculate total height of all nodes plus spacing
    const totalHeight = nodes.reduce((sum, node, index) => {
      const { height } = getNodeDimensions(node, compact);
      const gap = index < nodes.length - 1 ? spacing : 0;
      return sum + height + gap;
    }, 0);

    // Calculate max width for consistent left-edge alignment
    const maxWidth = Math.max(...nodes.map((n) => getNodeDimensions(n, compact).width));

    // Start Y position to center nodes vertically
    let currentY = -totalHeight / 2 + this.verticalMargin;

    return nodes.map((node) => {
      const { height } = getNodeDimensions(node, compact);

      // A dragged node still reserves its slot so later nodes do not shift up.
      const slot = {
        x: config.baseX - maxWidth / 2,
        y: currentY,
      };
      currentY += height + spacing;

      const preserved = preservedPositions?.get(node.id);
      return { ...node, position: preserved ?? slot };
    });
  }

  /**
   * Layout the servers zone as a column-major grid: the first `rows` nodes
   * fill column 0 top to bottom, the next `rows` column 1, and so on.
   *
   * The zone is centered on its nominal height, so its top edge depends only
   * on the server count and the card mode. Inside a column each card sits
   * its own height plus the gap below the previous one, so a card that
   * changes height moves only the cards below it in the same column.
   */
  private layoutServerGrid(
    nodes: Node[],
    config: ZoneConfig,
    columns: number,
    preservedPositions?: Map<string, { x: number; y: number }>,
    compact = false
  ): Node[] {
    if (nodes.length === 0) return [];

    const spacing = zoneSpacing(config, compact);
    const rows = Math.ceil(nodes.length / columns);
    const top = -stackHeight(rows, nominalCardHeight(compact), spacing) / 2 + this.verticalMargin;
    const maxWidth = Math.max(...nodes.map((n) => getNodeDimensions(n, compact).width));
    const left = config.baseX - maxWidth / 2;
    const pitch = LAYOUT.NODE_WIDTH + LAYOUT.SERVER_COLUMN_GAP;

    let currentY = top;
    return nodes.map((node, index) => {
      if (index % rows === 0) currentY = top;
      const column = Math.floor(index / rows);

      // A dragged node still reserves its slot so later nodes do not shift up.
      const slot = { x: left + column * pitch, y: currentY };
      currentY += getNodeDimensions(node, compact).height + spacing;

      const preserved = preservedPositions?.get(node.id);
      return { ...node, position: preserved ?? slot };
    });
  }
}

/**
 * Create a butterfly layout engine with default configuration
 */
export function createButterflyEngine(
  options?: ButterflyLayoutOptions
): ButterflyLayoutEngine {
  return new ButterflyLayoutEngine(options);
}
