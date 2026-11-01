import { describe, it, expect } from 'vitest';
import { COLORS } from '../lib/constants';
import {
  applyEdgeThemeColors,
  createAllEdges,
  createGatewayToSkillGroupEdges,
  type EnhancedEdge,
} from '../lib/graph/edges';
import type { ThemeColors } from '../themes/readThemeColors';
import type { AgentSkill, ClientStatus, MCPServerStatus, ResourceStatus } from '../types';

describe('edge animation defaults', () => {
  it('does not animate edges from the live producers or the skill-group producer', () => {
    const servers: MCPServerStatus[] = [{
      name: 'github',
      transport: 'http',
      initialized: true,
      toolCount: 1,
      tools: ['search'],
    }];
    const resources: ResourceStatus[] = [{ name: 'db', image: 'postgres', status: 'running' }];
    const clients: ClientStatus[] = [{
      name: 'Claude',
      slug: 'claude',
      detected: true,
      linked: true,
      transport: 'http',
    }];
    const skills: AgentSkill[] = [{
      name: 'branch',
      description: 'branch',
      state: 'active',
      fileCount: 1,
      dir: 'git/branch',
    }];

    for (const edge of [...createAllEdges(servers, resources, clients), ...createGatewayToSkillGroupEdges(skills)]) {
      expect(edge.animated).not.toBe(true);
    }
  });
});

describe('applyEdgeThemeColors', () => {
  it('rewrites marker colors by relation and leaves a markerless edge unchanged', () => {
    const colors = {
      ...COLORS,
      external: 'ext-color',
      secondary: 'sec-color',
      tertiary: 'ter-color',
      edgeDefault: 'edge-color',
    } satisfies ThemeColors;
    const marker = { type: 'arrowclosed' as const, width: 16, height: 16, color: 'stale' };
    const server: EnhancedEdge = {
      id: 'server',
      source: 'gateway',
      target: 'mcp',
      markerEnd: { ...marker },
      data: { relationType: 'gateway-to-server', isHighlightable: true },
    };
    const resource: EnhancedEdge = {
      id: 'resource',
      source: 'gateway',
      target: 'res',
      markerEnd: { ...marker },
      data: { relationType: 'gateway-to-resource', isHighlightable: false },
    };
    const skillGroup: EnhancedEdge = {
      id: 'skills',
      source: 'gateway',
      target: 'group',
      markerEnd: { ...marker },
      data: { relationType: 'gateway-to-skill-group', isHighlightable: false },
    };
    const other: EnhancedEdge = {
      id: 'tool',
      source: 'mcp',
      target: 'tool',
      markerEnd: { ...marker },
      data: { relationType: 'server-to-tool', isHighlightable: false },
    };
    const plain: EnhancedEdge = {
      id: 'client',
      source: 'client',
      target: 'gateway',
      data: { relationType: 'client-to-gateway', isHighlightable: true },
    };

    const [nextServer, nextResource, nextSkill, nextOther, nextPlain] = applyEdgeThemeColors(
      [server, resource, skillGroup, other, plain],
      colors,
    );

    expect(nextServer.markerEnd).toMatchObject({ color: 'ext-color' });
    expect(nextResource.markerEnd).toMatchObject({ color: 'sec-color' });
    expect(nextSkill.markerEnd).toMatchObject({ color: 'ter-color' });
    expect(nextOther.markerEnd).toMatchObject({ color: 'edge-color' });
    expect(nextPlain).toBe(plain);
  });
});
