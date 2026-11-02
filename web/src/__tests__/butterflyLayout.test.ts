import { describe, expect, it } from 'vitest';
import type { Edge, Node } from '@xyflow/react';

import { createButterflyEngine } from '../lib/graph/butterfly';
import { getNodeDimensions } from '../lib/graph/utils';
import { LAYOUT } from '../lib/constants';

// Zone nodeSpacing is hardcoded at 80 in butterfly.ts (lines 32-37), then
// halved in compact mode. LAYOUT.NODE_SPACING is not read by the engine.
const ZONE_NODE_SPACING = 80;

function clientNode(id: string): Node {
  return {
    id,
    type: 'client',
    position: { x: 0, y: 0 },
    data: { type: 'client', name: id },
  };
}

function gatewayNode(): Node {
  return {
    id: 'gateway',
    type: 'gateway',
    position: { x: 0, y: 0 },
    data: { type: 'gateway', name: 'gw' },
  };
}

function pitchFor(compact: boolean): number {
  const height = compact ? LAYOUT.CLIENT_HEIGHT_COMPACT : LAYOUT.CLIENT_HEIGHT;
  const spacing = compact ? Math.round(ZONE_NODE_SPACING * 0.5) : ZONE_NODE_SPACING;
  return height + spacing;
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

describe('ButterflyLayoutEngine preserved slots', () => {
  const engine = createButterflyEngine();

  it.each([
    { compact: false, pitch: 164 },
    { compact: true, pitch: 88 },
  ])(
    'reserves a dragged client slot so later clients stay put (compact=$compact)',
    ({ compact, pitch }) => {
      expect(pitchFor(compact)).toBe(pitch);

      const nodes = [
        gatewayNode(),
        clientNode('client-1'),
        clientNode('client-2'),
        clientNode('client-3'),
        clientNode('client-4'),
      ];
      const edges: Edge[] = [];
      const input = { nodes, edges };

      const first = engine.layout(input, { compact });
      expect(first.edges).toBe(input.edges);
      expect(first.nodes.map((node) => node.id)).toEqual(nodes.map((node) => node.id));
      for (const node of first.nodes) {
        expect(node.data).toBe(nodes.find((item) => item.id === node.id)?.data);
      }

      const clients = ['client-1', 'client-2', 'client-3', 'client-4'].map((id) => {
        const node = first.nodes.find((item) => item.id === id);
        expect(node).toBeDefined();
        return node!;
      });
      const clientX = clients[0].position.x;
      for (const client of clients) {
        expect(client.position.x).toBe(clientX);
      }
      for (let index = 1; index < clients.length; index += 1) {
        expect(clients[index].position.y - clients[index - 1].position.y).toBe(pitch);
      }

      const gateway = first.nodes.find((node) => node.id === 'gateway');
      expect(gateway).toBeDefined();

      const preservedPositions = new Map<string, { x: number; y: number }>([
        ['client-2', { x: clients[1].position.x, y: clients[1].position.y + 20 }],
        ['client-4', { x: clients[3].position.x, y: clients[3].position.y - 20 }],
      ]);

      const second = engine.layout(input, { compact, preservedPositions });
      expect(second.edges).toBe(input.edges);
      expect(second.nodes.map((node) => node.id)).toEqual(nodes.map((node) => node.id));

      const again = (id: string) => second.nodes.find((node) => node.id === id)!;
      expect(again('client-1').position).toEqual(clients[0].position);
      expect(again('client-3').position).toEqual(clients[2].position);
      expect(again('client-2').position).toEqual(preservedPositions.get('client-2'));
      expect(again('client-4').position).toEqual(preservedPositions.get('client-4'));
      expect(again('gateway').position).toEqual(gateway!.position);

      const stationary = [again('client-1'), again('client-3')];
      expect(overlaps(stationary[0], stationary[1], compact)).toBe(false);
    },
  );

  it.each([false, true])(
    'does not pull the column up when the first client is dragged (compact=%s)',
    (compact) => {
      const nodes = [
        gatewayNode(),
        clientNode('client-1'),
        clientNode('client-2'),
        clientNode('client-3'),
      ];
      const edges: Edge[] = [];
      const input = { nodes, edges };
      const first = engine.layout(input, { compact });
      const at = (id: string) => first.nodes.find((node) => node.id === id)!;
      const dragged = { x: at('client-1').position.x + 12, y: at('client-1').position.y + 30 };
      const preservedPositions = new Map([['client-1', dragged]]);

      const second = engine.layout(input, { compact, preservedPositions });
      const again = (id: string) => second.nodes.find((node) => node.id === id)!;
      expect(again('client-1').position).toEqual(dragged);
      expect(again('client-2').position).toEqual(at('client-2').position);
      expect(again('client-3').position).toEqual(at('client-3').position);
      expect(again('gateway').position).toEqual(at('gateway').position);
    },
  );
});
