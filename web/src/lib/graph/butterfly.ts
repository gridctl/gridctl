/**
 * Butterfly layout engine - Hub-and-Spoke with 4 zones
 *
 * Implements a clean, logic-driven layout where the Gateway acts as
 * the central hub with zones arranged left-to-right:
 *
 * Zone 0 (Left):      Clients - linked LLM clients
 * Zone 1 (Center):    Gateway - central hub/router
 * Zone 2 (Right):     MCP Servers - providers/tools
 * Zone 3 (Far Right): Resources - infrastructure/databases
 */

import type { Node } from '@xyflow/react';
import type {
  LayoutEngine,
  LayoutInput,
  LayoutOutput,
  LayoutOptions,
  ButterflyZone,
  ZoneConfig,
} from './types';
import { LAYOUT } from '../constants';
import { getNodeDimensions } from './utils';

/**
 * Default zone configuration
 * X positions create clear visual separation between zones
 * Zones: 0=CLIENTS, 1=GATEWAY, 2=SERVERS, 3=RESOURCES
 */
const DEFAULT_ZONE_CONFIGS: Record<ButterflyZone, ZoneConfig> = {
  [4]: { zone: 4, baseX: -400, nodeSpacing: 80 },       // CLIENTS (far left)
  [0]: { zone: 0, baseX: 0, nodeSpacing: 80 },          // AGENTS (left)
  [1]: { zone: 1, baseX: 400, nodeSpacing: 0 },         // GATEWAY (center)
  [2]: { zone: 2, baseX: 800, nodeSpacing: 80 },        // SERVERS (right)
  [3]: { zone: 3, baseX: 1200, nodeSpacing: 80 },       // RESOURCES (far right)
};

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

    // Calculate positioned nodes for each zone
    const positionedNodes: Node[] = [];

    for (const [zone, zoneNodes] of nodesByZone) {
      const config = this.zoneConfigs[zone];
      const positioned = this.layoutZone(zoneNodes, config, preservedPositions, compact);
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

    // Tighter spacing in compact mode — full gaps look excessive between short cards
    const spacing = compact ? Math.round(config.nodeSpacing * 0.5) : config.nodeSpacing;

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

      // Check for preserved position (user-dragged)
      const preserved = preservedPositions?.get(node.id);
      if (preserved) {
        return { ...node, position: preserved };
      }

      // Calculate position: left-align nodes within zone (all share same left edge)
      const position = {
        x: config.baseX - maxWidth / 2,
        y: currentY,
      };

      // Advance Y for next node
      currentY += height + spacing;

      return { ...node, position };
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
