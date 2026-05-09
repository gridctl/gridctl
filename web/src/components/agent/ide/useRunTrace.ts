import { useEffect, useState } from 'react';
import { subscribeToRunEvents, type RunEvent } from '../../../lib/agent-api';

/**
 * NodeStatus is the closed vocabulary of trace decorations the IDE
 * paints onto each node. The transitions are: queued → running →
 * (ok | error | suspended). A node only ever moves forward in this
 * lattice.
 */
export type NodeStatus = 'queued' | 'running' | 'ok' | 'error' | 'suspended';

export interface NodeTrace {
  status: NodeStatus;
  durationMicros?: number;
  model?: string;
  promptTokens?: number;
  outputTokens?: number;
  costUSD?: number;
  errorMessage?: string;
  spanID?: string;
  // Last seq seen for this node — useful for "Resume from here" so
  // the resume request lands on a coherent step boundary.
  lastSeq?: number;
}

export interface RunTrace {
  byNode: Record<string, NodeTrace>;
  events: RunEvent[];
  status: 'connecting' | 'open' | 'error' | 'idle';
}

interface NodeEnter {
  node_id?: string;
  node_name?: string;
  span_id?: string;
}
interface NodeExit {
  node_id?: string;
  duration_micros?: number;
  success?: boolean;
}
interface LLMCall {
  model?: string;
  prompt_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
}
interface ErrorPayload {
  node_id?: string;
  message?: string;
}
interface ApprovalRequest {
  approval_id?: string;
}

/**
 * useRunTrace subscribes to /api/agent/runs/{run_id}/events and
 * folds each event into a per-node decoration map. Idle (no
 * runID) returns an empty trace; the IDE renders the un-decorated
 * canvas in that mode.
 */
export function useRunTrace(runID: string | null): RunTrace {
  const [trace, setTrace] = useState<RunTrace>({
    byNode: {},
    events: [],
    status: runID ? 'connecting' : 'idle',
  });

  useEffect(() => {
    if (!runID) {
      setTrace({ byNode: {}, events: [], status: 'idle' });
      return;
    }
    setTrace({ byNode: {}, events: [], status: 'connecting' });
    let lastNodeID: string | null = null;
    const sub = subscribeToRunEvents(
      runID,
      (ev) => {
        setTrace((prev) => {
          const next: RunTrace = {
            byNode: { ...prev.byNode },
            events: [...prev.events, ev],
            status: 'open',
          };
          const payload = (ev.payload ?? {}) as Record<string, unknown>;
          switch (ev.type) {
            case 'node_enter': {
              const p = payload as unknown as NodeEnter;
              const id = p.node_id || p.node_name || '';
              if (id) {
                lastNodeID = id;
                next.byNode[id] = {
                  ...(next.byNode[id] ?? { status: 'queued' }),
                  status: 'running',
                  spanID: p.span_id,
                  lastSeq: ev.seq,
                };
              }
              break;
            }
            case 'node_exit': {
              const p = payload as unknown as NodeExit;
              const id = p.node_id || lastNodeID || '';
              if (id) {
                next.byNode[id] = {
                  ...(next.byNode[id] ?? { status: 'queued' }),
                  status: p.success ? 'ok' : 'error',
                  durationMicros: p.duration_micros,
                  lastSeq: ev.seq,
                };
              }
              break;
            }
            case 'llm_call': {
              const p = payload as unknown as LLMCall;
              const id = lastNodeID || '';
              if (id) {
                next.byNode[id] = {
                  ...(next.byNode[id] ?? { status: 'running' }),
                  model: p.model,
                  promptTokens: p.prompt_tokens,
                  outputTokens: p.output_tokens,
                  costUSD: p.cost_usd,
                  lastSeq: ev.seq,
                };
              }
              break;
            }
            case 'error': {
              const p = payload as unknown as ErrorPayload;
              const id = p.node_id || lastNodeID || '';
              if (id) {
                next.byNode[id] = {
                  ...(next.byNode[id] ?? { status: 'queued' }),
                  status: 'error',
                  errorMessage: p.message,
                  lastSeq: ev.seq,
                };
              }
              break;
            }
            case 'approval_request': {
              void (payload as unknown as ApprovalRequest);
              const id = lastNodeID || '';
              if (id) {
                next.byNode[id] = {
                  ...(next.byNode[id] ?? { status: 'queued' }),
                  status: 'suspended',
                  lastSeq: ev.seq,
                };
              }
              break;
            }
          }
          return next;
        });
      },
      () => {
        setTrace((prev) => ({ ...prev, status: 'error' }));
      },
    );
    return () => sub.close();
  }, [runID]);

  return trace;
}
