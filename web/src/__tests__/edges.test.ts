import { describe, it, expect } from 'vitest';
import { createAllEdges, createGatewayToSkillGroupEdges } from '../lib/graph/edges';
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
