import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { SkillEditor } from '../components/registry/SkillEditor';
import { createRegistrySkill, fetchRegistrySkill, updateRegistrySkill, validateSkill } from '../lib/api';
import { useUIStore } from '../stores/useUIStore';
import type { AgentSkill, SkillSourceStatus } from '../types';

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
  validateSkill: vi.fn().mockResolvedValue({ valid: true, errors: [], warnings: [] }),
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
const addCriterionButton = () => screen.getByRole('button', { name: 'Add acceptance criterion' });
const lastGiven = () => {
  const givens = screen.getAllByRole('textbox', { name: 'Given' });
  return givens[givens.length - 1];
};

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

  // Every call site clears `skill` on close, so the reopen goes through the
  // closed editor's '' seed. The second fetch differs from the first: a reopen
  // that adopts the earlier open's body would show '# Old' and write it back
  // over the saved edit.
  it('reloads the stored body when reopened on the same skill after a save', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...LIST_SKILL, body: '# Old' });
    const { rerender } = render(
      <SkillEditor isOpen onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />,
    );
    await waitFor(() => expect(bodyField()).toHaveValue('# Old'));

    fireEvent.change(bodyField(), { target: { value: '# Edited' } });
    fireEvent.click(saveButton());
    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1].body).toBe('# Edited');

    rerender(<SkillEditor isOpen={false} onClose={() => {}} onSaved={() => {}} />);
    let resolveFetch: (s: AgentSkill) => void = () => {};
    vi.mocked(fetchRegistrySkill).mockReturnValue(
      new Promise<AgentSkill>((resolve) => { resolveFetch = resolve; }),
    );
    rerender(<SkillEditor isOpen onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />);

    expect(saveButton()).toBeDisabled();
    expect(bodyField()).toHaveAttribute('readonly');
    expect(bodyField()).not.toHaveValue('# Old');
    expect(fetchRegistrySkill).toHaveBeenCalledTimes(2);

    resolveFetch({ ...LIST_SKILL, body: '# Edited' });
    await waitFor(() => expect(bodyField()).toHaveValue('# Edited'));
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.click(saveButton());
    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(2));
    expect(vi.mocked(updateRegistrySkill).mock.calls[1][1].body).toBe('# Edited');
  });

  // Same reopen, with the skill left set while closed.
  it('reloads the stored body when reopened with the skill still set', async () => {
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...LIST_SKILL, body: '# Old' });
    const { rerender } = render(
      <SkillEditor isOpen onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />,
    );
    await waitFor(() => expect(bodyField()).toHaveValue('# Old'));

    rerender(<SkillEditor isOpen={false} onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />);
    let resolveFetch: (s: AgentSkill) => void = () => {};
    vi.mocked(fetchRegistrySkill).mockReturnValue(
      new Promise<AgentSkill>((resolve) => { resolveFetch = resolve; }),
    );
    rerender(<SkillEditor isOpen onClose={() => {}} onSaved={() => {}} skill={LIST_SKILL} />);

    expect(saveButton()).toBeDisabled();
    expect(bodyField()).not.toHaveValue('# Old');

    resolveFetch({ ...LIST_SKILL, body: '# Changed elsewhere' });
    await waitFor(() => expect(bodyField()).toHaveValue('# Changed elsewhere'));
    expect(saveButton()).toBeEnabled();
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

const SOURCE: SkillSourceStatus = {
  name: 'team',
  repo: 'https://github.com/example/skills',
  autoUpdate: false,
  updateInterval: '',
  skills: [],
  updateAvailable: false,
  driftedSkills: ['incident-triage'],
};

describe('SkillEditor discard guard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...LIST_SKILL, body: '# Triage\n\nReal instructions.' });
  });

  afterEach(() => {
    useUIStore.setState({
      editorPrefs: { showFrontmatter: false, showPreview: true, splitRatio: 0.62 },
    });
  });

  it('closes a freshly hydrated skill without a discard prompt', async () => {
    const onClose = vi.fn();
    render(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} />);
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  // The reopened editor's baseline is the body fetched for this open, so a
  // hydrated reopen is clean even though the previous open saw other text.
  it('closes a reopened skill without a discard prompt once it has hydrated', async () => {
    const onClose = vi.fn();
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...LIST_SKILL, body: '# Old' });
    const { rerender } = render(
      <SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} />,
    );
    await waitFor(() => expect(bodyField()).toHaveValue('# Old'));

    rerender(<SkillEditor isOpen={false} onClose={onClose} onSaved={() => {}} />);
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...LIST_SKILL, body: '# Edited' });
    rerender(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} />);
    await waitFor(() => expect(bodyField()).toHaveValue('# Edited'));

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('confirms before discarding a body edit', async () => {
    const onClose = vi.fn();
    render(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} />);
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    fireEvent.change(bodyField(), { target: { value: '# Edited\n' } });
    fireEvent.keyDown(document, { key: 'Escape' });

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/unsaved edits/i);
    expect(bodyField()).toHaveValue('# Edited\n');

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onClose).not.toHaveBeenCalled();
    expect(bodyField()).toHaveValue('# Edited\n');

    fireEvent.keyDown(document, { key: 'Escape' });
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('confirms before discarding a frontmatter edit', async () => {
    // Seed the persisted pref instead of clicking Edit metadata. That click
    // updates the store from inside a setState updater and warns; the field
    // the editor shows is the same either way.
    useUIStore.setState({
      editorPrefs: { showFrontmatter: true, showPreview: true, splitRatio: 0.62 },
    });
    const onClose = vi.fn();
    render(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} />);
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    fireEvent.change(screen.getByPlaceholderText('Brief description of what this skill does'), {
      target: { value: 'Changed description' },
    });
    fireEvent.keyDown(document, { key: 'Escape' });

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/unsaved edits/i);
  });

  it('closes only the Reset confirm on Escape', async () => {
    const onClose = vi.fn();
    render(
      <SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} source={SOURCE} />,
    );
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    fireEvent.click(screen.getByRole('button', { name: 'Reset' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/upstream/i);

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('forks under the typed name with no supporting files', async () => {
    const onClose = vi.fn();
    render(
      <SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} source={SOURCE} />,
    );
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    fireEvent.click(screen.getByRole('button', { name: /fork as/i }));
    fireEvent.change(screen.getByPlaceholderText('new-skill-name'), { target: { value: 'triage-copy' } });
    fireEvent.click(screen.getByRole('button', { name: /^fork$/i }));

    await waitFor(() => expect(createRegistrySkill).toHaveBeenCalledTimes(1));
    expect(createRegistrySkill).toHaveBeenCalledWith(
      expect.objectContaining({
        name: 'triage-copy',
        fileCount: 0,
        body: '# Triage\n\nReal instructions.',
      }),
    );
  });

  it('asks before forking with an incomplete criterion and sends nothing on cancel', async () => {
    useUIStore.setState({
      editorPrefs: { showFrontmatter: true, showPreview: true, splitRatio: 0.62 },
    });
    render(
      <SkillEditor
        isOpen
        onClose={() => {}}
        onSaved={() => {}}
        skill={{ ...LIST_SKILL, body: '# Triage', acceptanceCriteria: ['Output is valid JSON'] }}
        source={SOURCE}
      />,
    );
    fireEvent.click(addCriterionButton());
    fireEvent.change(lastGiven(), { target: { value: 'only a given' } });

    fireEvent.click(screen.getByRole('button', { name: /fork as/i }));
    fireEvent.change(screen.getByPlaceholderText('new-skill-name'), { target: { value: 'triage-copy' } });
    fireEvent.click(screen.getByRole('button', { name: /^fork$/i }));

    const confirm = screen.getByRole('alertdialog');
    expect(confirm).toHaveTextContent(/1 acceptance criterion is incomplete/i);
    const cancel = within(confirm).getByRole('button', { name: 'Cancel' });
    await waitFor(() => expect(cancel).toHaveFocus());
    fireEvent.click(cancel);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(createRegistrySkill).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /^fork$/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Fork anyway' }));
    await waitFor(() => expect(createRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createRegistrySkill).mock.calls[0][0].acceptanceCriteria).toEqual(['Output is valid JSON']);
  });

  it('prevents unload while dirty and not while clean', async () => {
    const onClose = vi.fn();
    render(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={LIST_SKILL} />);
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage\n\nReal instructions.'));

    const clean = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);

    fireEvent.change(bodyField(), { target: { value: '# Edited\n' } });
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);
  });
});

// Free-form, lowercase, prefixed, PASS/FAIL, and multi-line criteria: every
// shape the old three-field parse rewrote or dropped.
const MIXED_CRITERIA = [
  'Output is valid JSON',
  'Given X, when Y, then Z',
  'Scenario 1: GIVEN a WHEN b THEN c',
  'GIVEN a WHEN b THEN c',
  'line one\nline two',
  'PASS: output is valid JSON',
  'PASS: GIVEN a WHEN b THEN c',
];

const MIXED_SKILL: AgentSkill = {
  ...LIST_SKILL,
  body: '# Triage',
  acceptanceCriteria: MIXED_CRITERIA,
};

describe('SkillEditor acceptance criteria round trip', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useUIStore.setState({
      editorPrefs: { showFrontmatter: true, showPreview: true, splitRatio: 0.62 },
    });
  });

  afterEach(() => {
    useUIStore.setState({
      editorPrefs: { showFrontmatter: false, showPreview: true, splitRatio: 0.62 },
    });
  });

  it('saves every criterion string unchanged after a body edit', async () => {
    renderEditor(MIXED_SKILL);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.change(bodyField(), { target: { value: '# Triage\n\nEdited.' } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1].acceptanceCriteria).toEqual(MIXED_CRITERIA);
  });

  it('opens clean, so closing shows no discard prompt', async () => {
    const onClose = vi.fn();
    render(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={MIXED_SKILL} />);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('opens clean after hydrating a body for a skill with free-form criteria', async () => {
    const listed: AgentSkill = { ...LIST_SKILL, acceptanceCriteria: MIXED_CRITERIA };
    vi.mocked(fetchRegistrySkill).mockResolvedValue({ ...listed, body: '# Triage' });
    const onClose = vi.fn();
    render(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={listed} />);
    await waitFor(() => expect(bodyField()).toHaveValue('# Triage'));

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('shows only canonical criteria as three fields and the rest as multi-line text', async () => {
    renderEditor(MIXED_SKILL);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    expect(screen.getAllByRole('textbox', { name: 'Given' })).toHaveLength(1);
    expect(screen.getByRole('textbox', { name: 'Given' })).toHaveValue('a');
    const multi = screen.getByRole('textbox', { name: 'Criterion 5' });
    expect(multi.tagName).toBe('TEXTAREA');
    expect(multi).toHaveValue('line one\nline two');
    expect(screen.getByRole('textbox', { name: 'Criterion 6' })).toHaveValue('PASS: output is valid JSON');
  });

  it('saves an edited free-form criterion at the same index', async () => {
    renderEditor(MIXED_SKILL);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.change(screen.getByRole('textbox', { name: 'Criterion 5' }), {
      target: { value: 'line one\nline two\nline three' },
    });
    fireEvent.click(saveButton());

    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    const expected = [...MIXED_CRITERIA];
    expected[4] = 'line one\nline two\nline three';
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1].acceptanceCriteria).toEqual(expected);
  });

  it('drops an emptied free-form criterion without asking', async () => {
    renderEditor(MIXED_SKILL);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.change(screen.getByRole('textbox', { name: 'Criterion 1' }), { target: { value: '' } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1].acceptanceCriteria).toEqual(MIXED_CRITERIA.slice(1));
  });

  it('marks an incomplete row and confirms before saving without it', async () => {
    renderEditor(MIXED_SKILL);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.click(addCriterionButton());
    expect(screen.queryByText(/^Incomplete:/)).not.toBeInTheDocument();
    fireEvent.change(lastGiven(), { target: { value: 'only a given' } });
    fireEvent.change(screen.getAllByRole('textbox', { name: 'When' }).at(-1)!, { target: { value: '' } });
    expect(screen.getByText(/^Incomplete: fill GIVEN, WHEN, and THEN/)).toBeInTheDocument();

    fireEvent.click(saveButton());
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/1 acceptance criterion is incomplete/i);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus());
    expect(updateRegistrySkill).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(updateRegistrySkill).not.toHaveBeenCalled();

    fireEvent.keyDown(document, { key: 's', ctrlKey: true });
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/will not be saved/i);
    expect(updateRegistrySkill).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Save anyway' }));
    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1].acceptanceCriteria).toEqual(MIXED_CRITERIA);
  });

  it('treats a partly filled row as unsaved work when closing', async () => {
    const onClose = vi.fn();
    render(<SkillEditor isOpen onClose={onClose} onSaved={() => {}} skill={MIXED_SKILL} />);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    fireEvent.click(addCriterionButton());
    fireEvent.change(lastGiven(), { target: { value: 'only a given' } });
    fireEvent.keyDown(document, { key: 'Escape' });

    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/unsaved edits/i);
  });

  it('saves a new skill with the untouched seed row without asking', async () => {
    renderNewEditor();
    fireEvent.change(screen.getByPlaceholderText('my-skill-name'), { target: { value: 'new-skill' } });
    fireEvent.change(screen.getByPlaceholderText('Brief description of what this skill does'), {
      target: { value: 'Does a thing' },
    });
    expect(screen.queryByText(/^Incomplete:/)).not.toBeInTheDocument();
    fireEvent.click(saveButton());

    await waitFor(() => expect(createRegistrySkill).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(vi.mocked(createRegistrySkill).mock.calls[0][0]).not.toHaveProperty('acceptanceCriteria');
  });

  it('saves a completed seed row in the canonical form', async () => {
    renderNewEditor();
    fireEvent.change(screen.getByPlaceholderText('my-skill-name'), { target: { value: 'new-skill' } });
    fireEvent.change(screen.getByPlaceholderText('Brief description of what this skill does'), {
      target: { value: 'Does a thing' },
    });
    fireEvent.change(lastGiven(), { target: { value: 'a request' } });
    fireEvent.change(screen.getByRole('textbox', { name: 'Then' }), { target: { value: 'it answers' } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(createRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(createRegistrySkill).mock.calls[0][0].acceptanceCriteria).toEqual([
      'GIVEN a request WHEN the skill is called THEN it answers',
    ]);
  });
});

describe('SkillEditor validation', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(validateSkill).mockResolvedValue({ valid: true, errors: [], warnings: [] });
    useUIStore.setState({
      editorPrefs: { showFrontmatter: true, showPreview: true, splitRatio: 0.62 },
    });
  });

  afterEach(() => {
    useUIStore.setState({
      editorPrefs: { showFrontmatter: false, showPreview: true, splitRatio: 0.62 },
    });
  });

  it('validates the save payload, description and extra unchanged', async () => {
    const withExtra: AgentSkill = {
      ...MIXED_SKILL,
      extra: { 'argument-hint': '<task>' },
    };
    renderEditor(withExtra);
    await waitFor(() => expect(saveButton()).toBeEnabled());

    const description = 'Trigger when user mentions: deploy';
    fireEvent.change(screen.getByPlaceholderText('Brief description of what this skill does'), {
      target: { value: description },
    });

    await waitFor(() =>
      expect(validateSkill).toHaveBeenLastCalledWith(expect.objectContaining({ description })),
    );
    const sent = vi.mocked(validateSkill).mock.calls.at(-1)![0];
    expect(sent.extra).toEqual({ 'argument-hint': '<task>' });
    expect(sent.acceptanceCriteria).toEqual(MIXED_CRITERIA);

    fireEvent.click(saveButton());
    await waitFor(() => expect(updateRegistrySkill).toHaveBeenCalledTimes(1));
    expect(vi.mocked(updateRegistrySkill).mock.calls[0][1]).toEqual(sent);
  });

  it('lists every server error and warning when the indicator is clicked', async () => {
    vi.mocked(validateSkill).mockResolvedValue({
      valid: false,
      errors: ['description is required'],
      warnings: ['body exceeds 500 lines (612)'],
    });
    renderEditor(MIXED_SKILL);

    const trigger = await screen.findByRole('button', { name: /validation messages/i });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByText('description is required')).not.toBeInTheDocument();

    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const panel = screen.getByRole('dialog', { name: 'Validation messages' });
    expect(panel).toHaveTextContent('description is required');
    expect(panel).toHaveTextContent('body exceeds 500 lines (612)');
  });

  it('lists the local incomplete-criteria warning even when the server result is clean', async () => {
    renderEditor(MIXED_SKILL);
    await waitFor(() => expect(validateSkill).toHaveBeenCalled());
    await screen.findByText('Valid');
    expect(screen.queryByRole('button', { name: /validation messages/i })).not.toBeInTheDocument();

    fireEvent.click(addCriterionButton());
    fireEvent.change(lastGiven(), { target: { value: 'only a given' } });

    fireEvent.click(screen.getByRole('button', { name: /validation messages/i }));
    expect(screen.getByRole('dialog', { name: 'Validation messages' })).toHaveTextContent(
      '1 incomplete acceptance criterion will not be saved',
    );
  });

  it('shows the local warning before any server result exists', () => {
    renderNewEditor();
    fireEvent.change(lastGiven(), { target: { value: 'only a given' } });
    // validateSkill fires only after the debounce; the indicator must not wait for it.
    expect(screen.getByRole('button', { name: /validation messages/i })).toHaveTextContent('1 warning(s)');
  });
});
