import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { SkillEditor } from '../components/registry/SkillEditor';
import { fetchRegistrySkill, updateRegistrySkill } from '../lib/api';
import type { AgentSkill } from '../types';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../components/registry/SkillFileTree', () => ({
  SkillFileTree: () => <div data-testid="file-tree" />,
}));

vi.mock('../lib/api', () => ({
  fetchRegistrySkill: vi.fn(),
  createRegistrySkill: vi.fn().mockResolvedValue({}),
  updateRegistrySkill: vi.fn().mockResolvedValue({}),
  validateSkillContent: vi.fn().mockResolvedValue({ valid: true, errors: [], warnings: [] }),
  resetSkill: vi.fn().mockResolvedValue({}),
  detachSkill: vi.fn().mockResolvedValue({}),
}));

// As delivered by GET /api/registry/skills: no Markdown body.
const LIST_SKILL: AgentSkill = {
  name: 'incident-triage',
  description: 'Triage incidents quickly',
  state: 'active',
  fileCount: 2,
  dir: 'ops/incident-triage',
};

function renderEditor(skill?: AgentSkill) {
  return render(
    <SkillEditor isOpen onClose={() => {}} onSaved={() => {}} skill={skill ?? LIST_SKILL} />,
  );
}

/** Renders the editor with no skill at all, the "New Skill" path. */
function renderNewEditor() {
  return render(<SkillEditor isOpen onClose={() => {}} onSaved={() => {}} />);
}

const saveButton = () => screen.getByRole('button', { name: /^save$/i });
// Queried by role rather than display value: the display-value matcher
// collapses whitespace, which a multi-line Markdown body will never survive.
const bodyField = () => screen.getByRole('textbox', { name: 'Skill instructions' });

describe('SkillEditor body hydration', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...LIST_SKILL, body: '# Triage\n\nReal instructions.' });
  });

  it('fetches the body for a skill that arrived without one', async () => {
    renderEditor();
    await waitFor(() => expect(fetchRegistrySkill).toHaveBeenCalledWith('incident-triage'));
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));
  });

  // The failure this guards against: `body` is '' until the fetch lands, so an
  // early save would write an empty body over the skill's real instructions.
  it('blocks saving until the body has loaded', async () => {
    let resolveFetch: (s: AgentSkill) => void = () => {};
    vi.mocked(fetchRegistrySkill).mockReturnValue(
      new Promise<AgentSkill>((resolve) => { resolveFetch = resolve; }),
    );
    renderEditor();

    expect(saveButton()).toBeDisabled();
    fireEvent.click(saveButton());
    expect(updateRegistrySkill).not.toHaveBeenCalled();

    resolveFetch({ ...LIST_SKILL, body: '# Triage\n\nReal instructions.' });
    await waitFor(() => expect(saveButton()).toBeEnabled());
  });

  // The invariant behind the guard: there must be no render in which Save is
  // clickable while the field still holds the pre-hydration empty string. An
  // effect-based hydration leaves exactly one such frame.
  it('never enables save before the body has landed in the field', async () => {
    renderEditor();
    await waitFor(() => expect(saveButton()).toBeEnabled());
    expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.');
  });

  it('saves the loaded body verbatim once hydration finishes', async () => {
    renderEditor();
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.click(saveButton());
    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1].body).toBe('# Triage\n\nReal instructions.');
  });

  it('keeps save blocked and explains itself when the body cannot be loaded', async () => {
    vi.mocked(fetchRegistrySkill).mockRejectedValue(new Error('registry offline'));
    renderEditor();

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/registry offline/i));
    expect(saveButton()).toBeDisabled();
    fireEvent.click(saveButton());
    expect(updateRegistrySkill).not.toHaveBeenCalled();
  });

  it('does not fetch for a new skill', async () => {
    renderNewEditor();
    await waitFor(() => expect(saveButton()).toBeDisabled()); // no name/description yet
    expect(fetchRegistrySkill).not.toHaveBeenCalled();
  });

  // Reopening resets every field, so a hydration keyed only on the skill name
  // would decline to re-adopt and leave the editor empty over real content.
  it('re-hydrates when the editor is reopened on the same skill', async () => {
    const { rerender } = render(
      <SkillEditor isOpen onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />,
    );
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    rerender(<SkillEditor isOpen={false} onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />);
    rerender(<SkillEditor isOpen onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />);

    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));
    expect(saveButton()).toBeEnabled();
  });

  it('does not fetch when the skill already carries a body', async () => {
    renderEditor({ ...LIST_SKILL, body: '# Already here' });
    await waitFor(() => expect(bodyField()).toHaveValue('# Already here'));
    expect(fetchRegistrySkill).not.toHaveBeenCalled();
    expect(saveButton()).toBeEnabled();
  });
});

describe('SkillEditor extra frontmatter pass-through', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('carries unmodeled frontmatter keys through a save untouched', async () => {
    const withExtra: AgentSkill = {
      ...LIST_SKILL,
      extra: { 'argument-hint': '<task description>', 'disable-model-invocation': true },
    };
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...withExtra, body: '# Triage\n' });
    renderEditor(withExtra);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.click(saveButton());
    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1].extra).toEqual({
      'argument-hint': '<task description>',
      'disable-model-invocation': true,
    });
  });

  it('omits the extra field entirely when the skill has none', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...LIST_SKILL, body: '# Triage\n' });
    renderEditor();
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.click(saveButton());
    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1]).not.toHaveProperty('extra');
  });
});
