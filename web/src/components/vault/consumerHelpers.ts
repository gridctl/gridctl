import type { Consumer } from '../../lib/api';

// A consumer is navigable to a topology node only when it is a server or a
// resource (those are the kinds the graph renders as nodes).
export function isNavigable(c: Consumer): boolean {
  return c.kind === 'mcp-server' || c.kind === 'resource';
}
