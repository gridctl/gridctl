import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ConsumerList } from '../components/vault/ConsumerList';
import type { Consumer } from '../lib/api';

const scoped = (target: string, targetKind: Consumer['kind'] = 'mcp-server'): Consumer => ({
  kind: 'secrets-set',
  name: 'dev',
  field: 'secrets.sets',
  target,
  targetKind,
});

const unscoped: Consumer = {
  kind: 'secrets-set',
  name: 'dev',
  field: 'secrets.sets',
};

const explicit: Consumer = {
  kind: 'mcp-server',
  name: 'github',
  field: 'env.TOKEN',
};

describe('ConsumerList', () => {
  it('renders an explicit site as a navigable row', () => {
    const onConsumerClick = vi.fn();
    render(
      <ConsumerList consumers={[explicit]} onConsumerClick={onConsumerClick} />,
    );

    const row = screen.getByRole('button', { name: /go to github/i });
    fireEvent.click(row);
    expect(onConsumerClick).toHaveBeenCalledWith(explicit);
  });

  it('renders an unscoped set injection as a non-navigable row', () => {
    const onConsumerClick = vi.fn();
    render(
      <ConsumerList consumers={[unscoped]} onConsumerClick={onConsumerClick} />,
    );

    expect(screen.getByText(/injected into server env/i)).toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('collapses a scoped set reaching several workloads into one summary row', () => {
    render(
      <ConsumerList
        consumers={[scoped('a'), scoped('b'), scoped('c')]}
        previewLimit={null}
      />,
    );

    // One summary row, not three near-identical ones.
    const summary = screen.getByRole('button', { expanded: false });
    expect(summary).toHaveTextContent('set: dev');
    expect(summary).toHaveTextContent('3 servers');
    expect(screen.queryByText(/into a$/)).not.toBeInTheDocument();
  });

  it('expands the summary into navigable per-workload rows', () => {
    const onConsumerClick = vi.fn();
    render(
      <ConsumerList
        consumers={[scoped('a'), scoped('b')]}
        previewLimit={null}
        onConsumerClick={onConsumerClick}
      />,
    );

    fireEvent.click(screen.getByRole('button', { expanded: false }));
    const child = screen.getByRole('button', { name: /go to a/i });
    fireEvent.click(child);
    expect(onConsumerClick).toHaveBeenCalledWith(scoped('a'));
  });

  it('pluralizes servers and resources separately', () => {
    render(
      <ConsumerList
        consumers={[scoped('a'), scoped('pg', 'resource')]}
        previewLimit={null}
      />,
    );

    const summary = screen.getByRole('button', { expanded: false });
    expect(summary).toHaveTextContent('1 server');
    expect(summary).toHaveTextContent('1 resource');
  });

  it('keeps a scope reaching one workload as a plain row', () => {
    render(<ConsumerList consumers={[scoped('a')]} previewLimit={null} />);
    // Nothing to expand, so no disclosure control.
    expect(screen.queryByRole('button', { expanded: false })).not.toBeInTheDocument();
    expect(screen.getByText(/into a/)).toBeInTheDocument();
  });

  it('collapses long lists behind a see-all toggle', () => {
    const many = ['a', 'b', 'c', 'd'].map((n) => ({
      kind: 'mcp-server' as const,
      name: n,
      field: 'env.X',
    }));
    const onToggleShowAll = vi.fn();
    render(
      <ConsumerList
        consumers={many}
        previewLimit={2}
        onToggleShowAll={onToggleShowAll}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: /see all 4/i }));
    expect(onToggleShowAll).toHaveBeenCalled();
  });
});
