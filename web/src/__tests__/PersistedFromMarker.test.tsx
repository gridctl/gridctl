import { beforeEach, describe, expect, it } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import '@testing-library/jest-dom';
import { PersistedFromMarker } from '../components/telemetry/PersistedFromMarker';
import { useTelemetryStore } from '../stores/useTelemetryStore';
import type { InventoryRecord, ResolvedTelemetry } from '../types';

function record(over: Partial<InventoryRecord> = {}): InventoryRecord {
  return {
    server: 'github',
    signal: 'metrics',
    path: '/tmp/metrics.jsonl',
    sizeBytes: 1024,
    oldestTime: '2026-11-17T08:00:00Z',
    newestTime: '2026-11-17T12:00:00Z',
    fileCount: 1,
    ...over,
  };
}

function seedTelemetry(config: ResolvedTelemetry, inventory: InventoryRecord[]) {
  useTelemetryStore.setState({ config, inventory, rawSpec: null, error: null });
}

beforeEach(() => {
  cleanup();
  seedTelemetry({ global: {}, servers: {} }, []);
});

describe('PersistedFromMarker', () => {
  it('appends the distinct server count in aggregated mode when the global signal is off', () => {
    seedTelemetry(
      {
        global: { logs: false, metrics: false, traces: false },
        servers: { github: { metrics: true } },
      },
      [record({ server: 'github', oldestTime: '2026-11-17T08:00:00Z' })],
    );

    render(<PersistedFromMarker serverName={null} signal="metrics" />);
    expect(screen.getByText(/persisted from 2026-11-17 · 1 server only/)).toBeInTheDocument();
  });

  it('counts distinct servers among effective records and pluralizes when more than one persists', () => {
    seedTelemetry(
      {
        global: { metrics: false },
        servers: {
          github: { metrics: true },
          jira: { metrics: true },
        },
      },
      [
        record({ server: 'github', oldestTime: '2026-11-17T08:00:00Z' }),
        // Second record for the same server should not double-count.
        record({ server: 'github', path: '/tmp/metrics.1.jsonl', oldestTime: '2026-11-16T08:00:00Z' }),
        // Record for a server without metrics enabled should be ignored.
        record({ server: 'stale', oldestTime: '2026-11-10T08:00:00Z' }),
        // Non-matching signal should be ignored.
        record({ server: 'jira', signal: 'runs', oldestTime: '2026-11-09T08:00:00Z' }),
      ],
    );

    const { rerender } = render(<PersistedFromMarker serverName={null} signal="metrics" />);
    expect(screen.getByText(/persisted from 2026-11-16 · 1 server only/)).toBeInTheDocument();

    seedTelemetry(
      {
        global: { metrics: false },
        servers: {
          github: { metrics: true },
          jira: { metrics: true },
        },
      },
      [
        record({ server: 'github', oldestTime: '2026-11-17T08:00:00Z' }),
        record({ server: 'jira', oldestTime: '2026-11-17T09:00:00Z' }),
      ],
    );
    rerender(<PersistedFromMarker serverName={undefined} signal="metrics" />);
    expect(screen.getByText(/persisted from 2026-11-17 · 2 servers only/)).toBeInTheDocument();
  });

  it('omits the suffix when the stack-global signal is on', () => {
    seedTelemetry(
      {
        global: { metrics: true },
        servers: { github: { metrics: true } },
      },
      [record({ server: 'github', oldestTime: '2026-11-17T08:00:00Z' })],
    );

    render(<PersistedFromMarker serverName={null} signal="metrics" />);
    const text = screen.getByText(/persisted from 2026-11-17/).textContent ?? '';
    expect(text).toContain('persisted from 2026-11-17');
    expect(text).not.toContain('only');
  });

  it('omits the suffix in single-server mode even when global is off', () => {
    seedTelemetry(
      {
        global: { metrics: false },
        servers: { github: { metrics: true } },
      },
      [record({ server: 'github', oldestTime: '2026-11-17T08:00:00Z' })],
    );

    render(<PersistedFromMarker serverName="github" signal="metrics" />);
    const text = screen.getByText(/persisted from 2026-11-17/).textContent ?? '';
    expect(text).toContain('persisted from 2026-11-17');
    expect(text).not.toContain('only');
  });
});
