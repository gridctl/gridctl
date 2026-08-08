import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { ModelChip, ModelHonorList } from '../ModelChip';
import { modelChipInfo } from '../../../lib/modelPreference';
import type { ModelPreference } from '../../../types';

const DECLARED_ONLY: ModelPreference = {
  declared: { value: 'opus', sourceKey: 'model' },
  honor: { 'claude-code': 'honored', antigravity: 'ignored', agents: 'unknown' },
};

const POLICY_OVERRIDE: ModelPreference = {
  declared: { value: 'opus', sourceKey: 'model' },
  resolved: { value: 'haiku', resolution: 'override' },
  honor: { 'claude-code': 'honored' },
};

const POLICY_DEFAULT_ONLY: ModelPreference = {
  resolved: { value: 'sonnet', resolution: 'default' },
  honor: { 'claude-code': 'honored', opencode: 'dropped-on-render' },
};

describe('modelChipInfo', () => {
  it('is null for absent or empty objects (older backends send nothing)', () => {
    expect(modelChipInfo(undefined)).toBeNull();
    expect(modelChipInfo(null)).toBeNull();
    expect(modelChipInfo({ honor: {} })).toBeNull();
  });

  it('prefers the policy-resolved value over the declaration', () => {
    const info = modelChipInfo(POLICY_OVERRIDE);
    expect(info).toMatchObject({ value: 'haiku', viaPolicy: true, resolution: 'override', declared: 'opus' });
  });

  it('falls back to the author declaration', () => {
    expect(modelChipInfo(DECLARED_ONLY)).toMatchObject({ value: 'opus', viaPolicy: false });
  });
});

describe('ModelChip', () => {
  it('renders nothing without a preference', () => {
    const { container } = render(<ModelChip modelPreference={undefined} />);
    expect(container).toBeEmptyDOMElement();
  });

  it('renders a plain chip for an author declaration', () => {
    render(<ModelChip modelPreference={DECLARED_ONLY} />);
    const chip = screen.getByTestId('model-chip');
    expect(chip).toHaveTextContent('opus');
    expect(chip).not.toHaveTextContent('policy');
    expect(chip.title).toContain('Author-declared');
  });

  it('renders the policy variant with provenance for a resolved value', () => {
    render(<ModelChip modelPreference={POLICY_OVERRIDE} />);
    const chip = screen.getByTestId('model-chip');
    expect(chip).toHaveTextContent('haiku');
    expect(chip).toHaveTextContent('policy');
    expect(chip.title).toContain('override');
    expect(chip.title).toContain('author declared opus');
  });

  it('handles a policy default with no declaration', () => {
    render(<ModelChip modelPreference={POLICY_DEFAULT_ONLY} />);
    const chip = screen.getByTestId('model-chip');
    expect(chip).toHaveTextContent('sonnet');
    expect(chip.title).toContain('default');
    expect(chip.title).toContain('author declared nothing');
  });
});

describe('ModelHonorList', () => {
  it('renders nothing without a matrix', () => {
    const { container } = render(<ModelHonorList honor={undefined} />);
    expect(container).toBeEmptyDOMElement();
    const { container: empty } = render(<ModelHonorList honor={{}} />);
    expect(empty).toBeEmptyDOMElement();
  });

  it('renders one row per target with the wire status vocabulary', () => {
    render(<ModelHonorList honor={DECLARED_ONLY.honor} />);
    const list = screen.getByTestId('model-honor-list');
    const items = list.querySelectorAll('li');
    expect(items).toHaveLength(3);
    // Sorted by slug: agents, antigravity, claude-code.
    expect(items[0]).toHaveTextContent('Agents interop dir');
    expect(items[0]).toHaveTextContent('unknown');
    expect(items[1]).toHaveTextContent('Antigravity');
    expect(items[1]).toHaveTextContent('ignored');
    expect(items[2]).toHaveTextContent('Claude Code');
    expect(items[2]).toHaveTextContent('honored');
  });

  it('labels dropped-on-render and falls back to the slug for unknown targets', () => {
    render(<ModelHonorList honor={{ 'some-new-client': 'dropped-on-render' }} />);
    const list = screen.getByTestId('model-honor-list');
    expect(list).toHaveTextContent('some-new-client');
    expect(list).toHaveTextContent('dropped on render');
  });
});
