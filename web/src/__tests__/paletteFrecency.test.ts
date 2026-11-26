import { describe, it, expect } from 'vitest';
import { FRECENCY_RENAMES, parseFrecency } from '../lib/paletteFrecency';

describe('parseFrecency', () => {
  it('credits renamed IDs to their new IDs', () => {
    const map = parseFrecency(
      JSON.stringify({
        'navigate:traces': { count: 3, lastUsed: 100 },
        'canvas:refresh': { count: 2, lastUsed: 200 },
        'appearance:dark': { count: 1, lastUsed: 50 },
      }),
    );
    expect(map.get('navigate:workspace-traces')).toEqual({ count: 3, lastUsed: 100 });
    expect(map.get('system:refresh')).toEqual({ count: 2, lastUsed: 200 });
    expect(map.get('appearance:dark')).toEqual({ count: 1, lastUsed: 50 });
    expect(map.has('navigate:traces')).toBe(false);
    expect(map.has('canvas:refresh')).toBe(false);
  });

  it('sums counts and keeps the later lastUsed when old and new keys both exist', () => {
    const map = parseFrecency(
      JSON.stringify({
        'navigate:logs': { count: 4, lastUsed: 500 },
        'navigate:workspace-logs': { count: 1, lastUsed: 300 },
        'canvas:refresh': { count: 1, lastUsed: 100 },
        'system:refresh': { count: 2, lastUsed: 900 },
      }),
    );
    expect(map.get('navigate:workspace-logs')).toEqual({ count: 5, lastUsed: 500 });
    expect(map.get('system:refresh')).toEqual({ count: 3, lastUsed: 900 });
    expect(map.size).toBe(2);
  });

  it('maps every listed rename', () => {
    expect(Object.keys(FRECENCY_RENAMES).sort()).toEqual(
      [
        'canvas:refresh',
        'navigate:connections',
        'navigate:logs',
        'navigate:metrics',
        'navigate:traces',
        'navigate:vault',
      ],
    );
    expect(FRECENCY_RENAMES['navigate:vault']).toBe('navigate:workspace-vault');
  });

  it.each([null, '', '{invalid json}', '[1,2]', 'null', '42'])('yields an empty map for %j', (raw) => {
    expect(parseFrecency(raw).size).toBe(0);
  });
});
