import { describe, it, expect, beforeEach } from 'vitest';
import { render, renderHook, act } from '@testing-library/react';
import { useTokenHeat } from '../hooks/useTokenHeat';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import type { TokenUsage } from '../types';

function usage(total: number): TokenUsage {
  return {
    session: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
    per_server: {
      github: { input_tokens: total, output_tokens: 0, total_tokens: total },
      jira: { input_tokens: 10, output_tokens: 0, total_tokens: 10 },
    },
    format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
  };
}

describe('useTokenHeat', () => {
  beforeEach(() => {
    useUIStore.setState({ showHeatMap: false });
    useStackStore.setState({ tokenUsage: null });
  });

  it('does not re-render when token usage changes while the heat map is off', () => {
    let renders = 0;
    function Probe() {
      renders += 1;
      useTokenHeat('github');
      return null;
    }
    render(<Probe />);
    const afterMount = renders;
    act(() => {
      useStackStore.setState({ tokenUsage: usage(40) });
    });
    expect(renders).toBe(afterMount);
  });

  it('computes intensity from the busiest server while the heat map is on', () => {
    useUIStore.setState({ showHeatMap: true });
    useStackStore.setState({ tokenUsage: usage(20) });
    const { result } = renderHook(() => useTokenHeat('github'));
    expect(result.current).toBe(1);
  });
});
