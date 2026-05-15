import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { DetachedSkillsSidebar } from '../components/skills/DetachedSkillsSidebar';
import type { SkillGraph, SkillSummary } from '../lib/agent-api';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
}));

vi.mock('../components/agent/ide/useRunTrace', () => ({
  useRunTrace: () => ({ byNode: {}, events: [], status: 'idle' }),
}));

const fetchSkill = vi.fn<(name: string) => Promise<SkillGraph | null>>();
const fetchSkills = vi.fn<() => Promise<SkillSummary[]>>();

vi.mock('../lib/agent-api', async () => {
  const actual = await vi.importActual<typeof import('../lib/agent-api')>('../lib/agent-api');
  return {
    ...actual,
    fetchSkill: (name: string) => fetchSkill(name),
    fetchSkills: () => fetchSkills(),
  };
});

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <DetachedSkillsSidebar />
    </MemoryRouter>,
  );
}

describe('DetachedSkillsSidebar', () => {
  beforeEach(() => {
    fetchSkill.mockReset();
    fetchSkills.mockReset();
  });

  it('shows the empty state when no skill param is provided', () => {
    renderAt('/sidebar?workspace=skills');
    expect(screen.getByText(/Provide a \?skill=/)).toBeInTheDocument();
    expect(fetchSkill).not.toHaveBeenCalled();
  });

  it('renders an error banner when the requested skill cannot be loaded', async () => {
    fetchSkill.mockResolvedValue(null);
    fetchSkills.mockResolvedValue([]);
    renderAt('/sidebar?workspace=skills&skill=nope');
    await waitFor(() => {
      expect(screen.getByText(/Skill unavailable/i)).toBeInTheDocument();
    });
  });

  it('renders NodeDetail for the requested node when the skill loads', async () => {
    const graph: SkillGraph = {
      skill: 'triage',
      lang: 'ts',
      file: 'triage.ts',
      nodes: [
        { id: 'tool_classify', kind: 'tool', label: 'classify', file: 'triage.ts', line: 10, col: 2 },
      ],
    };
    const summary: SkillSummary = {
      name: 'triage',
      lang: 'ts',
      dir: '/skills/triage',
      node_count: 1,
    };
    fetchSkill.mockResolvedValue(graph);
    fetchSkills.mockResolvedValue([summary]);

    renderAt('/sidebar?workspace=skills&skill=triage&node=tool_classify');
    // NodeDetail surfaces the node label + file:line; that's the contract
    // we care about for the detached window.
    await waitFor(() => {
      expect(screen.getByText('classify')).toBeInTheDocument();
    });
    expect(screen.getAllByText(/triage\.ts:10/).length).toBeGreaterThan(0);
  });
});
