import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';

vi.mock('../stores/useStackStore', () => ({
  useStackStore: vi.fn(),
}));

import { ToolList } from '../components/ui/ToolList';
import { useStackStore } from '../stores/useStackStore';

const mockTool = (name: string) => ({
  name,
  description: `Description of ${name}`,
  inputSchema: { type: 'object', properties: {} },
});

describe('ToolList', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows code mode message when code mode is active', () => {
    (useStackStore as unknown as ReturnType<typeof vi.fn>).mockImplementation((selector: (s: object) => unknown) =>
      selector({
        tools: [mockTool('search'), mockTool('execute')],
        codeMode: 'on',
      })
    );

    render(<ToolList serverName="github" />);

    expect(screen.getByText(/code mode active/i)).toBeInTheDocument();
    expect(screen.queryByText('No tools available')).not.toBeInTheDocument();
    expect(screen.queryByText('search')).not.toBeInTheDocument();
    expect(screen.queryByText('execute')).not.toBeInTheDocument();
  });

  it('shows tools normally when code mode is off', () => {
    (useStackStore as unknown as ReturnType<typeof vi.fn>).mockImplementation((selector: (s: object) => unknown) =>
      selector({
        tools: [mockTool('github__create_issue')],
        codeMode: null,
      })
    );

    render(<ToolList serverName="github" />);

    expect(screen.getByText('create_issue')).toBeInTheDocument();
    expect(screen.queryByText('No tools available')).not.toBeInTheDocument();
    expect(screen.queryByText(/code mode active/i)).not.toBeInTheDocument();
  });

  it('shows empty state when server has no tools and code mode is off', () => {
    (useStackStore as unknown as ReturnType<typeof vi.fn>).mockImplementation((selector: (s: object) => unknown) =>
      selector({
        tools: [],
        codeMode: null,
      })
    );

    render(<ToolList serverName="github" />);

    expect(screen.getByText('No tools available')).toBeInTheDocument();
    expect(screen.queryByText(/code mode active/i)).not.toBeInTheDocument();
  });
});
