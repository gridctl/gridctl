import { beforeEach, describe, expect, it } from 'vitest';
import { useToolCallHistoryStore } from '../stores/useToolCallHistoryStore';
import { useAuthStore } from '../stores/useAuthStore';

beforeEach(() => {
  useToolCallHistoryStore.getState().clear();
  localStorage.clear();
  sessionStorage.clear();
});

describe('useToolCallHistoryStore', () => {
  it('keeps the five most recent argument sets, most recent first', () => {
    const { push } = useToolCallHistoryStore.getState();
    for (let i = 1; i <= 6; i++) push('s__t', { n: i });
    expect(useToolCallHistoryStore.getState().list('s__t').map((e) => e.args)).toEqual([
      { n: 6 }, { n: 5 }, { n: 4 }, { n: 3 }, { n: 2 },
    ]);
  });

  it('de-duplicates by canonical JSON and moves the repeat to the front', () => {
    const { push } = useToolCallHistoryStore.getState();
    push('s__t', { a: 1, b: 2 });
    push('s__t', { c: 3 });
    push('s__t', { b: 2, a: 1 });
    expect(useToolCallHistoryStore.getState().list('s__t').map((e) => e.args)).toEqual([
      { b: 2, a: 1 }, { c: 3 },
    ]);
  });

  it('keeps each tool separate', () => {
    const { push } = useToolCallHistoryStore.getState();
    push('s__a', { x: 1 });
    push('s__b', { y: 2 });
    expect(useToolCallHistoryStore.getState().list('s__a').map((e) => e.args)).toEqual([{ x: 1 }]);
    expect(useToolCallHistoryStore.getState().list('s__b').map((e) => e.args)).toEqual([{ y: 2 }]);
    expect(useToolCallHistoryStore.getState().list('s__c')).toEqual([]);
  });

  it('never writes to web storage', () => {
    useToolCallHistoryStore.getState().push('s__t', { secret: 'hunter2' });
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  it('clears when the auth generation changes', () => {
    useToolCallHistoryStore.getState().push('s__t', { x: 1 });
    useAuthStore.setState({ generation: Symbol() });
    expect(useToolCallHistoryStore.getState().list('s__t')).toEqual([]);
  });
});
