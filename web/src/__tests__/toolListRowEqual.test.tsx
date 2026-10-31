import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { Command } from 'cmdk';
import {
  joinToolGroups,
  toolListRowPropsEqual,
  ToolListRow,
  type ToolListRowProps,
} from '../components/workspaces/ToolListRow';
import type { ToolRow } from '../hooks/useToolsEditor';

function row(over: Partial<ToolRow> = {}): ToolRow {
  return {
    name: 'list_repos',
    description: 'List repositories',
    annotations: { readOnlyHint: true, destructiveHint: false },
    ...over,
  };
}

function props(over: Partial<ToolListRowProps> = {}): ToolListRowProps {
  return {
    opt: row(),
    groups: joinToolGroups(['core']),
    lastUsed: '2m ago',
    isEnabled: true,
    isActive: false,
    auditState: 'used',
    policyLabel: '',
    policyTitle: '',
    policyTone: '',
    onSelect: () => {},
    onToggle: () => {},
    registerRef: () => {},
    ...over,
  };
}

describe('toolListRowPropsEqual', () => {
  const onSelect = () => {};
  const onToggle = () => {};
  const registerRef = () => {};
  const stable = { onSelect, onToggle, registerRef };

  it('treats a fresh opt object with the same rendered fields as equal', () => {
    const prev = props({ ...stable, opt: row() });
    const next = props({ ...stable, opt: row() });
    expect(prev.opt).not.toBe(next.opt);
    expect(toolListRowPropsEqual(prev, next)).toBe(true);
  });

  it('differs when name, description, groups, or lastUsed changes', () => {
    const base = props(stable);
    expect(toolListRowPropsEqual(base, props({ ...stable, opt: row({ name: 'other' }) }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, opt: row({ description: 'other' }) }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, groups: joinToolGroups(['other']) }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, lastUsed: '3m ago' }))).toBe(false);
  });

  it('compares annotations field-wise', () => {
    const base = props(stable);
    expect(toolListRowPropsEqual(base, props({
      ...stable,
      opt: row({ annotations: { readOnlyHint: true, destructiveHint: false } }),
    }))).toBe(true);
    expect(toolListRowPropsEqual(base, props({
      ...stable,
      opt: row({ annotations: { readOnlyHint: false, destructiveHint: false } }),
    }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({
      ...stable,
      opt: row({ annotations: { readOnlyHint: true, destructiveHint: true } }),
    }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({
      ...stable,
      opt: row({ annotations: { readOnlyHint: true, destructiveHint: false, title: 'List' } }),
    }))).toBe(false);
  });

  it('differs on primitives and callback identity', () => {
    const base = props(stable);
    expect(toolListRowPropsEqual(base, props({ ...stable, isEnabled: false }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, isActive: true }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, auditState: 'unused' }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, policyLabel: 'BLOCKED' }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, policyTitle: 'blocked by policy default' }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, policyTone: 'danger' }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, onSelect: () => {} }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, onToggle: () => {} }))).toBe(false);
    expect(toolListRowPropsEqual(base, props({ ...stable, registerRef: () => {} }))).toBe(false);
  });
});

describe('ToolListRow', () => {
  it('shows groups, annotation chips, and last-used text', () => {
    render(
      <Command shouldFilter={false}>
        <Command.List>
          <ToolListRow
            {...props({
              groups: joinToolGroups(['core', 'review']),
              lastUsed: '2m ago',
              auditState: 'used',
            })}
          />
        </Command.List>
      </Command>,
    );
    expect(screen.getByText('core')).toBeInTheDocument();
    expect(screen.getByText('review')).toBeInTheDocument();
    expect(screen.getByText('RO')).toBeInTheDocument();
    expect(screen.getByText('SAFE')).toBeInTheDocument();
    expect(screen.getByText(/2m ago/)).toBeInTheDocument();
    expect(screen.getByText('list_repos')).toBeInTheDocument();
  });

  it('follows the policy chip tone instead of a hardcoded danger class', () => {
    render(
      <Command shouldFilter={false}>
        <Command.List>
          <ToolListRow
            {...props({
              policyLabel: 'BLOCKED',
              policyTitle: 'blocked by policy default',
              policyTone: 'neutral',
            })}
          />
        </Command.List>
      </Command>,
    );
    const chip = screen.getByText('BLOCKED');
    expect(chip).toHaveClass('bg-surface-highlight');
    expect(chip).not.toHaveClass('text-status-error');
  });
});
