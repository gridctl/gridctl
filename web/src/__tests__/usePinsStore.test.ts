import { describe, it, expect, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  usePinsStore,
  useDriftedServers,
  firstDriftedServer,
  firstFindingsServer,
  countServerAlertFindings,
  serverHasAlertFindings,
  useFirstDriftedServer,
  useFirstFindingsServer,
} from '../stores/usePinsStore';

describe('useDriftedServers', () => {
  beforeEach(() => {
    usePinsStore.setState({ pins: null });
  });

  it('returns a stable reference when pins is null', () => {
    const { result, rerender } = renderHook(() => useDriftedServers());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });

  it('returns an empty array when pins is null', () => {
    const { result } = renderHook(() => useDriftedServers());
    expect(result.current).toHaveLength(0);
  });

  it('returns a stable reference when pins has no drifted servers', () => {
    act(() => {
      usePinsStore.setState({
        pins: {
          'my-server': {
            status: 'pinned',
            tool_count: 3,
            server_hash: 'abc',
            pinned_at: '2026-01-01T00:00:00Z',
            last_verified_at: '2026-01-01T00:00:00Z',
            tools: {},
          },
        },
      });
    });
    const { result, rerender } = renderHook(() => useDriftedServers());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });

  it('returns a stable reference when pins has drifted servers', () => {
    act(() => {
      usePinsStore.setState({
        pins: {
          'server-a': {
            status: 'drift',
            tool_count: 5,
            server_hash: 'def',
            pinned_at: '2026-01-01T00:00:00Z',
            last_verified_at: '2026-01-01T00:00:00Z',
            tools: {},
          },
        },
      });
    });
    const { result, rerender } = renderHook(() => useDriftedServers());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
  });

  it('returns drifted servers with correct shape', () => {
    act(() => {
      usePinsStore.setState({
        pins: {
          'server-a': {
            status: 'drift',
            tool_count: 5,
            server_hash: 'def',
            pinned_at: '2026-01-01T00:00:00Z',
            last_verified_at: '2026-01-02T00:00:00Z',
            tools: {},
          },
          'server-b': {
            status: 'pinned',
            tool_count: 2,
            server_hash: 'ghi',
            pinned_at: '2026-01-01T00:00:00Z',
            last_verified_at: '2026-01-01T00:00:00Z',
            tools: {},
          },
        },
      });
    });
    const { result } = renderHook(() => useDriftedServers());
    expect(result.current).toHaveLength(1);
    expect(result.current[0].name).toBe('server-a');
    expect(result.current[0].status).toBe('drift');
    expect(result.current[0].tool_count).toBe(5);
  });

  it('updates when pins changes from null to data', () => {
    const { result } = renderHook(() => useDriftedServers());
    expect(result.current).toHaveLength(0);

    act(() => {
      usePinsStore.setState({
        pins: {
          'server-a': {
            status: 'drift',
            tool_count: 3,
            server_hash: 'abc',
            pinned_at: '2026-01-01T00:00:00Z',
            last_verified_at: '2026-01-01T00:00:00Z',
            tools: {},
          },
        },
      });
    });

    expect(result.current).toHaveLength(1);
    expect(result.current[0].name).toBe('server-a');
  });
});

describe('deep-link selectors', () => {
  const server = (
    status: 'pinned' | 'drift',
    findingSeverity?: 'info' | 'warn' | 'critical',
  ) => ({
    status,
    tool_count: 1,
    server_hash: 'abc',
    pinned_at: '2026-01-01T00:00:00Z',
    last_verified_at: '2026-01-01T00:00:00Z',
    tools: {
      tool: {
        hash: 'h2:abc',
        name: 'tool',
        pinned_at: '2026-01-01T00:00:00Z',
        ...(findingSeverity
          ? {
              findings: [
                {
                  code: 'P001',
                  severity: findingSeverity,
                  confidence: 'high' as const,
                  field: 'description',
                  message: 'msg',
                },
              ],
            }
          : {}),
      },
    },
  });

  beforeEach(() => {
    usePinsStore.setState({ pins: null });
  });

  it('firstDriftedServer picks the alphabetically first drifted server', () => {
    expect(firstDriftedServer(null)).toBeNull();
    expect(firstDriftedServer({ a: server('pinned') })).toBeNull();
    expect(
      firstDriftedServer({ zeta: server('drift'), alpha: server('drift'), mid: server('pinned') }),
    ).toBe('alpha');
  });

  it('firstFindingsServer requires warn or critical findings', () => {
    expect(firstFindingsServer(null)).toBeNull();
    expect(firstFindingsServer({ a: server('pinned', 'info') })).toBeNull();
    expect(
      firstFindingsServer({ z: server('pinned', 'warn'), b: server('pinned', 'critical') }),
    ).toBe('b');
  });

  it('countServerAlertFindings excludes info findings', () => {
    expect(countServerAlertFindings(server('pinned', 'info'))).toBe(0);
    expect(countServerAlertFindings(server('pinned', 'warn'))).toBe(1);
  });

  it('serverHasAlertFindings requires warn or critical', () => {
    expect(serverHasAlertFindings(server('pinned'))).toBe(false);
    expect(serverHasAlertFindings(server('pinned', 'info'))).toBe(false);
    expect(serverHasAlertFindings(server('pinned', 'warn'))).toBe(true);
    expect(serverHasAlertFindings(server('pinned', 'critical'))).toBe(true);
  });

  it('useFirstDriftedServer is referentially stable across rerenders', () => {
    act(() => {
      usePinsStore.setState({ pins: { alpha: server('drift') } });
    });
    const { result, rerender } = renderHook(() => useFirstDriftedServer());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
    expect(first).toBe('alpha');
  });

  it('useFirstFindingsServer is referentially stable across rerenders', () => {
    act(() => {
      usePinsStore.setState({ pins: { alpha: server('pinned', 'warn') } });
    });
    const { result, rerender } = renderHook(() => useFirstFindingsServer());
    const first = result.current;
    rerender();
    expect(result.current).toBe(first);
    expect(first).toBe('alpha');
  });
});
