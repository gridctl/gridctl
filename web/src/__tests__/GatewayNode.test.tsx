import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

// React Flow primitives are mocked the same way the other node-component tests
// do, so the node can render outside a real <ReactFlow> provider.
vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
}));

import GatewayNode from '../components/graph/GatewayNode';
import type { GatewayNodeData } from '../types';

const baseData: GatewayNodeData = {
  type: 'gateway',
  name: 'acme-stack',
  version: 'v9.9.9',
  serverCount: 2,
  resourceCount: 0,
  clientCount: 0,
  totalToolCount: 3,
  sessions: 0,
  codeMode: 'on',
  totalSkills: 0,
  activeSkills: 0,
};

function renderNode(data: GatewayNodeData = baseData) {
  return render(<GatewayNode data={data} />);
}

describe('GatewayNode', () => {
  it('renders Code Mode as read-only status, not an action', () => {
    renderNode();
    const codeMode = screen.getByText('Code Mode').closest('[role="status"]');
    expect(codeMode).not.toBeNull();
    // A status pill must not carry action affordances.
    expect(codeMode).not.toHaveAttribute('onclick');
    expect(codeMode!.tagName).toBe('SPAN');
    expect(codeMode!.className).not.toMatch(/cursor-pointer/);
    expect(codeMode!.className).not.toMatch(/hover:/);
  });

  it('omits the Code Mode pill when code mode is off', () => {
    renderNode({ ...baseData, codeMode: 'off' });
    expect(screen.queryByText('Code Mode')).not.toBeInTheDocument();
  });

  it('keeps Gateway Active as a read-only status pill', () => {
    renderNode();
    const active = screen.getByText('Gateway Active').closest('[role="status"]');
    expect(active).not.toBeNull();
    expect(active!.className).not.toMatch(/cursor-pointer/);
  });
});
