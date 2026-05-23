import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { SetGroup, type SetGroupRowHandlers } from '../components/vault/SetGroup';

const handlers: SetGroupRowHandlers = {
  revealed: {},
  editingKey: null,
  editValue: '',
  showEditValue: false,
  setNames: [],
  onReveal: () => {},
  onEdit: () => {},
  onDeleteSecret: () => {},
  onEditValueChange: () => {},
  onEditToggleShow: () => {},
  onEditSave: () => {},
  onEditCancel: () => {},
  onAssignSet: () => {},
};

function renderGroup(recentlyEdited?: boolean) {
  return render(
    <SetGroup
      set={{ name: 'dev', count: 2 }}
      variables={[]}
      expanded={false}
      onToggleExpand={() => {}}
      recentlyEdited={recentlyEdited}
      handlers={handlers}
    />,
  );
}

describe('SetGroup — recently edited dot', () => {
  it('renders the dot with title and aria-label when recentlyEdited is true', () => {
    renderGroup(true);
    const dot = screen.getByTitle('Recently edited');
    expect(dot).toBeInTheDocument();
    // Meaning must not be conveyed by color alone.
    expect(dot).toHaveAttribute('aria-label', 'Recently edited');
  });

  it('renders no dot when recentlyEdited is false', () => {
    renderGroup(false);
    expect(screen.queryByTitle('Recently edited')).not.toBeInTheDocument();
  });

  it('renders no dot when recentlyEdited is omitted', () => {
    render(
      <SetGroup
        set={{ name: 'dev', count: 2 }}
        variables={[]}
        expanded={false}
        onToggleExpand={() => {}}
        handlers={handlers}
      />,
    );
    expect(screen.queryByTitle('Recently edited')).not.toBeInTheDocument();
  });
});
