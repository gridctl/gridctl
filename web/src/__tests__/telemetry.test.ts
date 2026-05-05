import { describe, it, expect } from 'vitest';
import {
  effectiveSignal,
  nextTriState,
  parseStackTelemetry,
  readTriState,
  triStateToPatch,
  type TriState,
} from '../lib/telemetry-config';
import { buildPillLabel } from '../components/telemetry/HeaderTelemetryPill';
import { _testFormatScope } from '../components/telemetry/WipeTelemetryDialog';
import type { InventoryRecord, ResolvedTelemetry } from '../types';

const STACK_YAML = `version: "1"
name: example
telemetry:
  persist:
    logs: true
    metrics: false
    traces: true
  retention:
    max_size_mb: 100
    max_backups: 5
    max_age_days: 7
mcp-servers:
  - name: github
    transport: http
    url: https://api.github.com/mcp
    telemetry:
      persist:
        traces: false
  - name: filesystem
    transport: http
    url: https://example.com/fs
`;

describe('parseStackTelemetry', () => {
  it('extracts global persist defaults', () => {
    const cfg = parseStackTelemetry(STACK_YAML);
    expect(cfg.global).toEqual({ logs: true, metrics: false, traces: true });
  });

  it('extracts retention block', () => {
    const cfg = parseStackTelemetry(STACK_YAML);
    expect(cfg.retention).toEqual({ max_size_mb: 100, max_backups: 5, max_age_days: 7 });
  });

  it('extracts per-server overrides only when present', () => {
    const cfg = parseStackTelemetry(STACK_YAML);
    expect(cfg.servers.github).toEqual({ traces: false });
    expect(cfg.servers.filesystem).toBeUndefined();
  });

  it('returns empty config for null/empty input', () => {
    expect(parseStackTelemetry(null)).toEqual({ global: {}, servers: {} });
    expect(parseStackTelemetry('')).toEqual({ global: {}, servers: {} });
  });

  it('returns empty config for malformed YAML rather than throwing', () => {
    const cfg = parseStackTelemetry('this: is: invalid: yaml: [unclosed');
    expect(cfg).toEqual({ global: {}, servers: {} });
  });

  it('ignores non-object telemetry blocks gracefully', () => {
    const cfg = parseStackTelemetry('telemetry: "wrong-shape"\n');
    expect(cfg).toEqual({ global: {}, servers: {} });
  });
});

describe('effectiveSignal — inheritance', () => {
  const cfg: ResolvedTelemetry = parseStackTelemetry(STACK_YAML);

  it('returns explicit override when set', () => {
    expect(effectiveSignal(cfg, 'github', 'traces')).toBe(false);
  });

  it('falls back to stack-global when override is absent', () => {
    expect(effectiveSignal(cfg, 'github', 'logs')).toBe(true);
    expect(effectiveSignal(cfg, 'filesystem', 'logs')).toBe(true);
  });

  it('returns false when neither override nor global is set', () => {
    const empty: ResolvedTelemetry = { global: {}, servers: {} };
    expect(effectiveSignal(empty, 'unknown', 'logs')).toBe(false);
  });
});

describe('readTriState + nextTriState — cycling state machine', () => {
  const cfg: ResolvedTelemetry = parseStackTelemetry(STACK_YAML);

  it('reads "off" when an explicit-false override exists', () => {
    expect(readTriState(cfg, 'github', 'traces')).toBe('off');
  });

  it('reads "inherit" when no per-server override exists', () => {
    expect(readTriState(cfg, 'filesystem', 'logs')).toBe('inherit');
  });

  it('cycles inherit -> on -> off -> inherit', () => {
    let s: TriState = 'inherit';
    s = nextTriState(s);
    expect(s).toBe('on');
    s = nextTriState(s);
    expect(s).toBe('off');
    s = nextTriState(s);
    expect(s).toBe('inherit');
  });

  it('maps tri-state to PATCH body values', () => {
    expect(triStateToPatch('inherit')).toBe(null);
    expect(triStateToPatch('on')).toBe(true);
    expect(triStateToPatch('off')).toBe(false);
  });

  it('cycle composes with patch mapping for the full state machine', () => {
    // inherit (null) -> on (true) -> off (false) -> inherit (null)
    expect(triStateToPatch(nextTriState('inherit'))).toBe(true);
    expect(triStateToPatch(nextTriState('on'))).toBe(false);
    expect(triStateToPatch(nextTriState('off'))).toBe(null);
  });
});

describe('buildPillLabel — header pill formatting', () => {
  it('returns Off when no signals are enabled', () => {
    expect(buildPillLabel({})).toBe('Persistence: Off');
    expect(buildPillLabel({ logs: false, metrics: false, traces: false })).toBe('Persistence: Off');
  });

  it('renders single signal', () => {
    expect(buildPillLabel({ logs: true })).toBe('Persistence: Logs');
  });

  it('renders pair joined by + in canonical order', () => {
    // Order matters: we want "Logs+Traces" regardless of input order to
    // match the canonical SIGNALS list (logs, metrics, traces).
    expect(buildPillLabel({ traces: true, logs: true })).toBe('Persistence: Logs+Traces');
  });

  it('renders all three abbreviations', () => {
    expect(buildPillLabel({ logs: true, metrics: true, traces: true })).toBe(
      'Persistence: Logs+Metrics+Traces',
    );
  });
});

describe('WipeTelemetryDialog scope formatting', () => {
  const records: InventoryRecord[] = [
    {
      server: 'github',
      signal: 'logs',
      path: '/tmp/logs.jsonl',
      sizeBytes: 1024 * 1024 * 100, // 100 MiB
      oldestTime: '2026-04-30T00:00:00Z',
      newestTime: '2026-05-04T12:00:00Z',
      fileCount: 3,
    },
    {
      server: 'github',
      signal: 'traces',
      path: '/tmp/traces.jsonl',
      sizeBytes: 1024 * 1024 * 42, // 42 MiB
      oldestTime: '2026-04-29T00:00:00Z',
      newestTime: '2026-05-04T11:00:00Z',
      fileCount: 1,
    },
    {
      server: 'filesystem',
      signal: 'metrics',
      path: '/tmp/metrics.jsonl',
      sizeBytes: 1024 * 50,
      oldestTime: '2026-05-01T00:00:00Z',
      newestTime: '2026-05-04T13:00:00Z',
      fileCount: 1,
    },
  ];

  it('aggregates total bytes, server count, and signal list', () => {
    const view = _testFormatScope(records);
    expect(view.totalBytes).toBe('142 MiB');
    expect(view.servers).toBe(2);
    // Signals always render in canonical order regardless of insertion.
    expect(view.signals).toEqual(['logs', 'metrics', 'traces']);
  });

  it('renders span as oldest -> newest dates', () => {
    const view = _testFormatScope(records);
    expect(view.span).toBe('2026-04-29 → 2026-05-04');
  });

  it('returns empty/null span for no records', () => {
    const view = _testFormatScope([]);
    expect(view.totalBytes).toBe('0 B');
    expect(view.servers).toBe(0);
    expect(view.signals).toEqual([]);
    expect(view.span).toBe(null);
  });
});

describe('StackModifiedError — 409 surfacing', () => {
  it('parses the 409 envelope into a typed error with hint', async () => {
    // Reach into the api module after stubbing global fetch so the helper
    // builds and throws StackModifiedError on the stack_modified envelope.
    const { updateStackTelemetry, StackModifiedError } = await import('../lib/api');
    const originalFetch = globalThis.fetch;
    globalThis.fetch = (() =>
      Promise.resolve(
        new Response(
          JSON.stringify({
            error: {
              code: 'stack_modified',
              message: 'The stack file was modified outside the canvas.',
              hint: 'Reload the file and try again.',
            },
          }),
          { status: 409, headers: { 'content-type': 'application/json' } },
        ),
      )) as typeof fetch;

    let caught: unknown = null;
    try {
      await updateStackTelemetry({ persist: { logs: true } });
    } catch (err) {
      caught = err;
    } finally {
      globalThis.fetch = originalFetch;
    }

    expect(caught).toBeInstanceOf(StackModifiedError);
    const err = caught as InstanceType<typeof StackModifiedError>;
    expect(err.code).toBe('stack_modified');
    expect(err.hint).toBe('Reload the file and try again.');
  });
});
