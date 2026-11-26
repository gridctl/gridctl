import { useMemo } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { buildStackRows, type StackRow } from '../lib/stackRows';

/** Stack table rows, rebuilt only when the store's node array changes identity. */
export function useStackRows(): StackRow[] {
  const nodes = useStackStore((s) => s.nodes);
  return useMemo(() => buildStackRows(nodes ?? []), [nodes]);
}
