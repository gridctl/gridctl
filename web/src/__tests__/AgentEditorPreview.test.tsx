import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { AgentEditor } from '../components/registry/agents/AgentEditor';
import { bodyFromRaw } from '../components/registry/agents/agentModel';
import { fetchRegistryAgent } from '../lib/api';
import type { RegistryAgent } from '../types';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchRegistryAgent: vi.fn(),
    updateRegistryAgent: vi.fn(),
  };
});

vi.mock('../components/registry/agents/agentModel', async () => {
  const actual = await vi.importActual<typeof import('../components/registry/agents/agentModel')>(
    '../components/registry/agents/agentModel',
  );
  return {
    ...actual,
    bodyFromRaw: vi.fn((raw: string) => actual.bodyFromRaw(raw)),
  };
});

const agent: RegistryAgent = {
  name: 'code-reviewer',
  description: 'Reviews code',
};

const raw = '---\nname: code-reviewer\ndescription: d\n---\nBody.';

describe('AgentEditor preview body', () => {
  beforeEach(() => {
    vi.mocked(bodyFromRaw).mockClear();
    vi.mocked(fetchRegistryAgent).mockResolvedValue({ ...agent, raw });
  });

  it('computes bodyFromRaw once per raw value', async () => {
    const { rerender } = render(
      <AgentEditor isOpen agent={agent} onClose={() => {}} onSaved={() => {}} />,
    );
    const textarea = await screen.findByRole('textbox', { name: /AGENT.md content/ });
    await waitFor(() => {
      expect((textarea as HTMLTextAreaElement).value).toContain('code-reviewer');
    });

    const afterLoad = vi.mocked(bodyFromRaw).mock.calls.length;
    expect(afterLoad).toBeGreaterThan(0);

    rerender(<AgentEditor isOpen agent={agent} onClose={() => {}} onSaved={() => {}} />);
    expect(vi.mocked(bodyFromRaw).mock.calls.length).toBe(afterLoad);

    fireEvent.change(textarea, {
      target: { value: '---\nname: code-reviewer\ndescription: d\n---\nEdited body.' },
    });
    expect(vi.mocked(bodyFromRaw).mock.calls.length).toBe(afterLoad + 1);

    fireEvent.click(screen.getByRole('button', { name: 'Preview' }));
    expect(screen.getByText(/Edited body/)).toBeInTheDocument();
    expect(vi.mocked(bodyFromRaw).mock.calls.length).toBe(afterLoad + 1);
  });
});
