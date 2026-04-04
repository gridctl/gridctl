import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { ServerPins } from '../lib/api';

interface PinsState {
  pins: Record<string, ServerPins> | null;
  setPins: (pins: Record<string, ServerPins>) => void;
}

export const usePinsStore = create<PinsState>()(
  subscribeWithSelector((set) => ({
    pins: null,
    setPins: (pins) => set({ pins }),
  }))
);

export const useDriftedServers = () =>
  usePinsStore((s) => {
    if (!s.pins) return [];
    return Object.entries(s.pins)
      .filter(([, sp]) => sp.status === 'drift')
      .map(([name, sp]) => ({ name, ...sp }));
  });
