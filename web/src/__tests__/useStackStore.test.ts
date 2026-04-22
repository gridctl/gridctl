import { describe, it, expect } from 'vitest';
import {
  updateAutoscaleObservability,
  AUTOSCALE_HISTORY_CAP,
  AUTOSCALE_DECISIONS_CAP,
  type AutoscaleSample,
  type AutoscaleDecision,
} from '../stores/useStackStore';
import type { MCPServerStatus, AutoscaleStatus } from '../types';

function makeServer(overrides: Partial<MCPServerStatus> = {}, autoscale?: AutoscaleStatus): MCPServerStatus {
  return {
    name: 'srv-a',
    transport: 'http',
    initialized: true,
    toolCount: 0,
    tools: [],
    autoscale,
    ...overrides,
  };
}

function as(overrides: Partial<AutoscaleStatus> = {}): AutoscaleStatus {
  return {
    min: 1,
    max: 5,
    current: 1,
    target: 1,
    targetInFlight: 10,
    medianInFlight: 5,
    lastDecision: 'noop',
    ...overrides,
  };
}

describe('updateAutoscaleObservability — ring buffer', () => {
  it('appends a sample per server on each poll', () => {
    const server = makeServer({}, as({ current: 2, target: 3, medianInFlight: 7 }));
    const { history } = updateAutoscaleObservability([server], {}, {}, {});
    expect(history['srv-a']).toHaveLength(1);
    expect(history['srv-a'][0]).toMatchObject({ current: 2, target: 3, medianInFlight: 7 });
  });

  it('caps the ring buffer at AUTOSCALE_HISTORY_CAP and evicts oldest', () => {
    let history: Record<string, AutoscaleSample[]> = {};
    const lastSeen: Record<string, { upAt?: string; downAt?: string }> = {};

    for (let i = 0; i < AUTOSCALE_HISTORY_CAP; i++) {
      const server = makeServer({}, as({ current: i, target: 1 }));
      ({ history } = updateAutoscaleObservability([server], history, {}, lastSeen));
    }
    expect(history['srv-a']).toHaveLength(AUTOSCALE_HISTORY_CAP);
    expect(history['srv-a'][0].current).toBe(0); // oldest

    // One more poll — 121st sample should evict the first (current=0).
    const server = makeServer({}, as({ current: 999, target: 1 }));
    ({ history } = updateAutoscaleObservability([server], history, {}, lastSeen));
    expect(history['srv-a']).toHaveLength(AUTOSCALE_HISTORY_CAP);
    expect(history['srv-a'][0].current).toBe(1); // first evicted
    expect(history['srv-a'][AUTOSCALE_HISTORY_CAP - 1].current).toBe(999);
  });

  it('does not append samples for servers without autoscale', () => {
    const server = makeServer();
    const { history } = updateAutoscaleObservability([server], {}, {}, {});
    expect(history['srv-a']).toBeUndefined();
  });
});

describe('updateAutoscaleObservability — decision feed', () => {
  it('records nothing on first observation (establishes baseline)', () => {
    const server = makeServer({}, as({ lastScaleUpAt: '2026-04-22T12:00:00Z' }));
    const { decisions, lastSeen } = updateAutoscaleObservability([server], {}, {}, {});
    expect(decisions['srv-a']).toBeUndefined();
    expect(lastSeen['srv-a']).toEqual({ upAt: '2026-04-22T12:00:00Z', downAt: undefined });
  });

  it('prepends a decision entry on lastScaleUpAt change', () => {
    const first = makeServer({}, as({ current: 1, lastScaleUpAt: '2026-04-22T12:00:00Z' }));
    const step1 = updateAutoscaleObservability([first], {}, {}, {});

    const second = makeServer(
      {},
      as({
        current: 2,
        medianInFlight: 18,
        targetInFlight: 10,
        lastScaleUpAt: '2026-04-22T12:00:03Z',
      }),
    );
    const step2 = updateAutoscaleObservability([second], step1.history, step1.decisions, step1.lastSeen);

    expect(step2.decisions['srv-a']).toHaveLength(1);
    expect(step2.decisions['srv-a'][0]).toMatchObject({
      kind: 'up',
      from: 1,
      to: 2,
    });
    expect(step2.decisions['srv-a'][0].reason).toMatch(/18/);
  });

  it('prepends a decision entry on lastScaleDownAt change', () => {
    const first = makeServer({}, as({ current: 3, lastScaleDownAt: '2026-04-22T12:00:00Z' }));
    const step1 = updateAutoscaleObservability([first], {}, {}, {});

    const second = makeServer(
      {},
      as({
        current: 2,
        medianInFlight: 2,
        targetInFlight: 10,
        lastScaleDownAt: '2026-04-22T12:00:03Z',
      }),
    );
    const step2 = updateAutoscaleObservability([second], step1.history, step1.decisions, step1.lastSeen);
    expect(step2.decisions['srv-a']).toHaveLength(1);
    expect(step2.decisions['srv-a'][0]).toMatchObject({ kind: 'down', from: 3, to: 2 });
  });

  it('caps the decision feed at AUTOSCALE_DECISIONS_CAP per server', () => {
    let history: Record<string, AutoscaleSample[]> = {};
    let decisions: Record<string, AutoscaleDecision[]> = {};
    let lastSeen: Record<string, { upAt?: string; downAt?: string }> = {};

    // First poll establishes baseline; subsequent polls each advance the
    // timestamp and record one decision. Need CAP + 3 advancing polls after
    // the baseline to overflow the feed.
    const totalPolls = AUTOSCALE_DECISIONS_CAP + 4;
    for (let i = 0; i < totalPolls; i++) {
      const server = makeServer(
        {},
        as({
          current: i + 1,
          lastScaleUpAt: `2026-04-22T12:00:${String(i).padStart(2, '0')}Z`,
        }),
      );
      ({ history, decisions, lastSeen } = updateAutoscaleObservability(
        [server],
        history,
        decisions,
        lastSeen,
      ));
    }
    expect(decisions['srv-a']).toHaveLength(AUTOSCALE_DECISIONS_CAP);
  });

  it('does not append duplicate decisions when lastScaleUpAt is unchanged', () => {
    const server = makeServer({}, as({ current: 2, lastScaleUpAt: '2026-04-22T12:00:00Z' }));
    const step1 = updateAutoscaleObservability([server], {}, {}, {});
    const step2 = updateAutoscaleObservability([server], step1.history, step1.decisions, step1.lastSeen);
    expect(step2.decisions['srv-a']).toBeUndefined();
  });
});
