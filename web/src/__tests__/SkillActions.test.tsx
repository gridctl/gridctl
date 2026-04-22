import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent } from '@testing-library/react';
import { SkillActions } from '../components/registry/SkillActions';
import type { AgentSkill } from '../types';

beforeEach(() => {
  cleanup();
});

function skill(overrides: Partial<AgentSkill> = {}): AgentSkill {
  return {
    name: 'test-skill',
    description: '',
    state: 'active',
    dir: 'test-skill',
    body: '',
    fileCount: 0,
    ...overrides,
  } as AgentSkill;
}

describe('SkillActions', () => {
  it('renders a disable button for an active skill', () => {
    render(
      <SkillActions
        skill={skill({ state: 'active' })}
        onToggle={() => {}}
        onEdit={() => {}}
        onDelete={() => {}}
      />,
    );
    expect(screen.getByTitle('Disable skill')).toBeInTheDocument();
  });

  it('renders an activate button for a non-active skill', () => {
    render(
      <SkillActions
        skill={skill({ state: 'disabled' })}
        onToggle={() => {}}
        onEdit={() => {}}
        onDelete={() => {}}
      />,
    );
    expect(screen.getByTitle('Activate skill')).toBeInTheDocument();
  });

  it('invokes onToggle when the power button is clicked', () => {
    const onToggle = vi.fn();
    const s = skill({ state: 'active' });
    render(
      <SkillActions
        skill={s}
        onToggle={onToggle}
        onEdit={() => {}}
        onDelete={() => {}}
      />,
    );
    fireEvent.click(screen.getByTitle('Disable skill'));
    expect(onToggle).toHaveBeenCalledWith(s);
  });

  it('invokes onEdit when the edit button is clicked', () => {
    const onEdit = vi.fn();
    const s = skill();
    render(
      <SkillActions
        skill={s}
        onToggle={() => {}}
        onEdit={onEdit}
        onDelete={() => {}}
      />,
    );
    fireEvent.click(screen.getByTitle('Edit skill'));
    expect(onEdit).toHaveBeenCalledWith(s);
  });

  it('invokes onDelete when the delete button is clicked', () => {
    const onDelete = vi.fn();
    const s = skill();
    render(
      <SkillActions
        skill={s}
        onToggle={() => {}}
        onEdit={() => {}}
        onDelete={onDelete}
      />,
    );
    fireEvent.click(screen.getByTitle('Delete skill'));
    expect(onDelete).toHaveBeenCalledWith(s);
  });

  it('hides the toggle when showToggle is false', () => {
    render(
      <SkillActions
        skill={skill({ state: 'active' })}
        onToggle={() => {}}
        onEdit={() => {}}
        onDelete={() => {}}
        showToggle={false}
      />,
    );
    expect(screen.queryByTitle('Disable skill')).toBeNull();
    expect(screen.queryByTitle('Activate skill')).toBeNull();
    expect(screen.getByTitle('Edit skill')).toBeInTheDocument();
    expect(screen.getByTitle('Delete skill')).toBeInTheDocument();
  });
});
