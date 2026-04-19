import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { useState } from 'react';
import { ToolsPicker } from '../components/wizard/steps/ToolsPicker';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import type { Tool } from '../types';
import * as apiModule from '../lib/api';
import { ProbeError } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

const mockStoreState: { tools: Tool[] } = { tools: [] };

vi.mock('../stores/useStackStore', () => ({
  useStackStore: vi.fn((selector: (s: { tools: Tool[] }) => unknown) =>
    selector(mockStoreState),
  ),
}));

const SERVER = 'db';

function tool(name: string, description?: string): Tool {
  return {
    name: `${SERVER}${TOOL_NAME_DELIMITER}${name}`,
    description,
    inputSchema: {},
  };
}

function Harness({
  serverName = SERVER,
  initial = [] as string[],
  onChangeSpy,
}: {
  serverName?: string;
  initial?: string[];
  onChangeSpy?: (v: string[]) => void;
}) {
  const [value, setValue] = useState<string[]>(initial);
  return (
    <ToolsPicker
      serverName={serverName}
      value={value}
      onChange={(v) => {
        setValue(v);
        onChangeSpy?.(v);
      }}
    />
  );
}

beforeEach(() => {
  mockStoreState.tools = [];
});

describe('ToolsPicker', () => {
  describe('checklist mode', () => {
    beforeEach(() => {
      mockStoreState.tools = [
        tool('query', 'Run a SQL query'),
        tool('insert', 'Insert a row'),
        tool('delete_row', 'Delete a row by id'),
      ];
    });

    it('renders an option for every topology tool and strips the server prefix', () => {
      render(<Harness />);
      expect(screen.getByRole('option', { name: 'query' })).toBeInTheDocument();
      expect(screen.getByRole('option', { name: 'insert' })).toBeInTheDocument();
      expect(screen.getByRole('option', { name: 'delete_row' })).toBeInTheDocument();
    });

    it('shows "0 of 3 selected" when nothing is chosen', () => {
      render(<Harness />);
      const header = screen.getByText(/selected — empty means all tools exposed/);
      expect(header).toHaveTextContent('0 of 3');
    });

    it('pre-checks tools already in value (backward compat with existing stacks)', () => {
      render(<Harness initial={['query']} />);
      expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute(
        'aria-checked',
        'true',
      );
      expect(screen.getByRole('option', { name: 'insert' })).toHaveAttribute(
        'aria-checked',
        'false',
      );
    });

    it('toggles selection on click and emits the new array via onChange', () => {
      const onChange = vi.fn();
      render(<Harness onChangeSpy={onChange} />);
      fireEvent.click(screen.getByRole('option', { name: 'query' }));
      expect(onChange).toHaveBeenLastCalledWith(['query']);
      fireEvent.click(screen.getByRole('option', { name: 'insert' }));
      expect(onChange).toHaveBeenLastCalledWith(['query', 'insert']);
      // Toggling off removes it
      fireEvent.click(screen.getByRole('option', { name: 'query' }));
      expect(onChange).toHaveBeenLastCalledWith(['insert']);
    });

    it('"Select all" selects every visible tool', () => {
      const onChange = vi.fn();
      render(<Harness onChangeSpy={onChange} />);
      fireEvent.click(screen.getByRole('button', { name: /select all visible tools/i }));
      expect(onChange).toHaveBeenLastCalledWith(
        expect.arrayContaining(['query', 'insert', 'delete_row']),
      );
      expect(onChange.mock.calls.at(-1)![0]).toHaveLength(3);
    });

    it('"Clear" deselects everything', () => {
      const onChange = vi.fn();
      render(<Harness initial={['query', 'insert']} onChangeSpy={onChange} />);
      fireEvent.click(screen.getByRole('button', { name: /clear all selected tools/i }));
      expect(onChange).toHaveBeenLastCalledWith([]);
    });

    it('fuzzy search narrows the visible list and "Select all" only affects visible', () => {
      const onChange = vi.fn();
      render(<Harness onChangeSpy={onChange} />);
      const input = screen.getByPlaceholderText('Search tools...');
      fireEvent.change(input, { target: { value: 'query' } });
      // Only the matching tool remains visible
      expect(screen.getByRole('option', { name: 'query' })).toBeInTheDocument();
      expect(screen.queryByRole('option', { name: 'insert' })).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: /select all visible tools/i }));
      expect(onChange).toHaveBeenLastCalledWith(['query']);
    });

    it('shows an empty-filter message when no tool matches the search query', () => {
      render(<Harness />);
      const input = screen.getByPlaceholderText('Search tools...');
      fireEvent.change(input, { target: { value: 'zzz-nonexistent' } });
      expect(screen.getByText(/No tools match/)).toBeInTheDocument();
    });
  });

  describe('empty state', () => {
    it('shows neutral empty copy (not an error) when no topology tools and no prior selections', () => {
      render(<Harness />);
      expect(screen.getByText(/No tools found for/)).toBeInTheDocument();
      // Not styled/phrased as an error
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });

    it('exposes manual entry within one click from the empty state', () => {
      render(<Harness />);
      fireEvent.click(
        screen.getByRole('button', { name: /enter tool names manually/i }),
      );
      // Now the manual-entry "Add tool" button is visible
      expect(screen.getByRole('button', { name: /add tool/i })).toBeInTheDocument();
    });
  });

  describe('manual-entry mode', () => {
    it('defaults to manual mode when no topology tools but prior values exist', () => {
      render(<Harness initial={['custom_tool']} />);
      // The manual-entry input is visible
      expect(screen.getByRole('textbox', { name: /tool 1/i })).toHaveValue('custom_tool');
    });

    it('adds and removes tools in manual mode and emits updated arrays', () => {
      const onChange = vi.fn();
      render(<Harness initial={['first']} onChangeSpy={onChange} />);
      fireEvent.click(screen.getByRole('button', { name: /add tool/i }));
      expect(onChange).toHaveBeenLastCalledWith(['first', '']);

      const input2 = screen.getByRole('textbox', { name: /tool 2/i });
      fireEvent.change(input2, { target: { value: 'second' } });
      expect(onChange).toHaveBeenLastCalledWith(['first', 'second']);

      fireEvent.click(screen.getByRole('button', { name: /remove tool 1/i }));
      expect(onChange).toHaveBeenLastCalledWith(['second']);
    });

    it('"Back to search" returns to checklist mode when topology tools are available', () => {
      mockStoreState.tools = [tool('query')];
      render(<Harness />);
      // Start in checklist; toggle into manual
      fireEvent.click(
        screen.getByRole('button', { name: /enter tool names manually/i }),
      );
      expect(screen.queryByRole('option', { name: 'query' })).not.toBeInTheDocument();
      // Back to search
      fireEvent.click(screen.getByRole('button', { name: /back to search/i }));
      expect(screen.getByRole('option', { name: 'query' })).toBeInTheDocument();
    });

    it('does not offer "Back to search" when topology has no tools', () => {
      render(<Harness initial={['custom']} />);
      expect(
        screen.queryByRole('button', { name: /back to search/i }),
      ).not.toBeInTheDocument();
    });
  });

  describe('probe integration', () => {
    beforeEach(() => {
      vi.spyOn(apiModule, 'probeServer');
    });
    afterEach(() => {
      vi.restoreAllMocks();
    });

    it('hides the Discover tools button when no probeConfig is provided', () => {
      render(<Harness />);
      expect(
        screen.queryByRole('button', { name: /discover tools/i }),
      ).not.toBeInTheDocument();
    });

    it('hides the Discover tools button for unsupported transports', () => {
      render(
        <ToolsPicker
          serverName="x"
          value={[]}
          onChange={() => {}}
          probeConfig={{ ssh: { host: 'h', user: 'u' }, command: ['/bin/sh'] }}
        />,
      );
      expect(
        screen.queryByRole('button', { name: /discover tools/i }),
      ).not.toBeInTheDocument();
    });

    it('clicking Discover tools populates the checklist on success', async () => {
      vi.mocked(apiModule.probeServer).mockResolvedValueOnce({
        tools: [
          { name: 'hello', description: 'say hi', inputSchema: {} },
          { name: 'goodbye', description: 'say bye', inputSchema: {} },
        ],
        probedAt: new Date().toISOString(),
        cached: false,
      });

      render(
        <ToolsPicker
          serverName="x"
          value={[]}
          onChange={() => {}}
          probeConfig={{ url: 'https://example.com/mcp' }}
        />,
      );

      fireEvent.click(
        screen.getByRole('button', { name: /discover tools by probing/i }),
      );
      // The discovered tools appear as checklist options.
      await waitFor(() => {
        expect(screen.getByRole('option', { name: 'hello' })).toBeInTheDocument();
      });
      expect(screen.getByRole('option', { name: 'goodbye' })).toBeInTheDocument();
    });

    it('renders an inline error with Retry on probe failure and keeps manual-entry reachable', async () => {
      vi.mocked(apiModule.probeServer).mockRejectedValueOnce(
        new ProbeError('initialize_failed', 'Server failed to initialize', 'check env', 422),
      );

      render(
        <ToolsPicker
          serverName="x"
          value={[]}
          onChange={() => {}}
          probeConfig={{ url: 'https://example.com/mcp' }}
        />,
      );
      fireEvent.click(
        screen.getByRole('button', { name: /discover tools by probing/i }),
      );

      await waitFor(() => {
        expect(screen.getByRole('alert')).toBeInTheDocument();
      });
      expect(screen.getByRole('alert')).toHaveTextContent('Server failed to initialize');
      expect(screen.getByRole('alert')).toHaveTextContent('check env');
      expect(
        screen.getByRole('button', { name: /retry probing the server/i }),
      ).toBeInTheDocument();
      // Manual-entry fallback is still offered
      expect(
        screen.getByRole('button', { name: /enter tool names manually/i }),
      ).toBeInTheDocument();
    });

    it('invalid_config errors do NOT show a Retry (the form needs fixing)', async () => {
      vi.mocked(apiModule.probeServer).mockRejectedValueOnce(
        new ProbeError('invalid_config', 'Config incomplete', undefined, 400),
      );

      render(
        <ToolsPicker
          serverName="x"
          value={[]}
          onChange={() => {}}
          probeConfig={{ url: 'https://example.com/mcp' }}
        />,
      );
      fireEvent.click(
        screen.getByRole('button', { name: /discover tools by probing/i }),
      );
      await waitFor(() => {
        expect(screen.getByRole('alert')).toBeInTheDocument();
      });
      expect(
        screen.queryByRole('button', { name: /retry probing the server/i }),
      ).not.toBeInTheDocument();
    });

    it('sets aria-busy on the picker while a probe is in flight', () => {
      let resolve: (v: apiModule.ProbeSuccess) => void = () => {};
      vi.mocked(apiModule.probeServer).mockReturnValueOnce(
        new Promise((r) => {
          resolve = r;
        }),
      );

      const { container } = render(
        <ToolsPicker
          serverName="x"
          value={[]}
          onChange={() => {}}
          probeConfig={{ url: 'https://example.com/mcp' }}
        />,
      );
      fireEvent.click(
        screen.getByRole('button', { name: /discover tools by probing/i }),
      );
      const picker = container.querySelector('[aria-label="Tools Picker"]');
      expect(picker).toHaveAttribute('aria-busy', 'true');

      // Let the promise resolve so the test doesn't leak a pending fetch.
      resolve({ tools: [], probedAt: new Date().toISOString(), cached: false });
    });
  });

  describe('persistence of selection across store re-renders', () => {
    it('keeps selection when topology tools update (simulates step navigation / rehydration)', () => {
      mockStoreState.tools = [tool('query'), tool('insert')];
      const { rerender } = render(<Harness initial={['query']} />);
      expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute(
        'aria-checked',
        'true',
      );
      // Simulate topology gaining a new tool (e.g., after deploy) — selection persists
      mockStoreState.tools = [tool('query'), tool('insert'), tool('vacuum')];
      rerender(<Harness initial={['query']} />);
      expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute(
        'aria-checked',
        'true',
      );
    });

    it('surfaces selected names that are not in the topology so they can be unchecked', () => {
      mockStoreState.tools = [tool('query')];
      render(<Harness initial={['query', 'legacy_tool']} />);
      expect(screen.getByRole('option', { name: 'query' })).toBeInTheDocument();
      expect(screen.getByRole('option', { name: 'legacy_tool' })).toHaveAttribute(
        'aria-checked',
        'true',
      );
    });
  });
});
