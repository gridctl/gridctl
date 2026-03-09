import { describe, it, expect, beforeEach } from 'vitest';

// --- parseLogEntry and formatTimestamp (pure function tests) ---

import { parseLogEntry, formatTimestamp } from '../components/log/logTypes';

describe('parseLogEntry', () => {
  it('parses JSON log entry', () => {
    const input = JSON.stringify({ level: 'INFO', msg: 'started', ts: '2026-01-01T00:00:00Z' });
    const result = parseLogEntry(input);
    expect(result.level).toBe('INFO');
    expect(result.message).toBe('started');
    expect(result.timestamp).toBe('2026-01-01T00:00:00Z');
  });

  it('parses slog text format', () => {
    const input = 'time=2026-01-01T00:00:00Z level=WARN msg="disk full" component=storage';
    const result = parseLogEntry(input);
    expect(result.level).toBe('WARN');
    expect(result.message).toBe('disk full');
    expect(result.attrs?.component).toBe('storage');
  });

  it('parses plain text with level detection', () => {
    const result = parseLogEntry('ERROR something went wrong');
    expect(result.level).toBe('ERROR');
    expect(result.message).toBe('ERROR something went wrong');
  });

  it('defaults to INFO for unknown levels in JSON', () => {
    const input = JSON.stringify({ level: 'TRACE', msg: 'trace msg' });
    const result = parseLogEntry(input);
    expect(result.level).toBe('INFO');
  });

  it('defaults to DEBUG for plain text without level keyword', () => {
    const result = parseLogEntry('some random log line');
    expect(result.level).toBe('DEBUG');
  });

  it('handles LogEntry object input', () => {
    const entry = { level: 'ERROR', msg: 'crash', ts: '2026-01-01T00:00:00Z', component: 'api' };
    const result = parseLogEntry(entry as never);
    expect(result.level).toBe('ERROR');
    expect(result.message).toBe('crash');
    expect(result.component).toBe('api');
  });

  it('strips Docker timestamp prefix', () => {
    const input = '2026-02-03T15:22:01.637603230Z time=2026-02-03T15:22:01Z level=INFO msg="ready"';
    const result = parseLogEntry(input);
    expect(result.level).toBe('INFO');
    expect(result.message).toBe('ready');
  });

  it('preserves raw input', () => {
    const input = 'hello world';
    const result = parseLogEntry(input);
    expect(result.raw).toBe(input);
  });
});

describe('formatTimestamp', () => {
  it('returns empty string for empty input', () => {
    expect(formatTimestamp('')).toBe('');
  });

  it('formats valid ISO timestamp', () => {
    const result = formatTimestamp('2026-01-15T14:30:45.123Z');
    // Result should contain time components
    expect(result).toMatch(/\d{2}:\d{2}:\d{2}\.\d{3}/);
  });

  it('handles invalid timestamp gracefully', () => {
    // Should fall through to the slice fallback
    const result = formatTimestamp('not-a-date-at-all-but-long-enough');
    expect(typeof result).toBe('string');
  });
});

// --- useRegistryStore (Zustand store tests) ---

import { useRegistryStore } from '../stores/useRegistryStore';
import type { AgentSkill, RegistryStatus } from '../types';

describe('useRegistryStore', () => {
  beforeEach(() => {
    useRegistryStore.setState({
      skills: [],
      status: { totalSkills: 0, activeSkills: 0 },
      isLoading: false,
      error: null,
    });
  });

  it('sets skills', () => {
    const skills: AgentSkill[] = [
      { name: 'test', description: 'desc', state: 'active', body: '', fileCount: 0 },
    ];
    useRegistryStore.getState().setSkills(skills);
    expect(useRegistryStore.getState().skills).toEqual(skills);
  });

  it('sets status', () => {
    const status: RegistryStatus = { totalSkills: 5, activeSkills: 3 };
    useRegistryStore.getState().setStatus(status);
    expect(useRegistryStore.getState().status).toEqual(status);
  });

  it('sets loading state', () => {
    useRegistryStore.getState().setLoading(true);
    expect(useRegistryStore.getState().isLoading).toBe(true);
  });

  it('sets error', () => {
    useRegistryStore.getState().setError('Something broke');
    expect(useRegistryStore.getState().error).toBe('Something broke');
  });

  it('hasContent returns true when skills exist', () => {
    useRegistryStore.getState().setSkills([
      { name: 'test', description: 'desc', state: 'active', body: '', fileCount: 0 },
    ]);
    expect(useRegistryStore.getState().hasContent()).toBe(true);
  });

  it('hasContent returns false when empty', () => {
    expect(useRegistryStore.getState().hasContent()).toBe(false);
  });

  it('activeSkillCount counts only active skills', () => {
    useRegistryStore.getState().setSkills([
      { name: 'a', description: '', state: 'active', body: '', fileCount: 0 },
      { name: 'b', description: '', state: 'disabled', body: '', fileCount: 0 },
      { name: 'c', description: '', state: 'active', body: '', fileCount: 0 },
    ]);
    expect(useRegistryStore.getState().activeSkillCount()).toBe(2);
  });
});

// --- useAuthStore (Zustand store tests) ---

import { useAuthStore } from '../stores/useAuthStore';

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.setState({ authRequired: false, isAuthenticated: false });
  });

  it('sets authRequired', () => {
    useAuthStore.getState().setAuthRequired(true);
    expect(useAuthStore.getState().authRequired).toBe(true);
  });

  it('sets isAuthenticated', () => {
    useAuthStore.getState().setAuthenticated(true);
    expect(useAuthStore.getState().isAuthenticated).toBe(true);
  });

  it('defaults to not authenticated', () => {
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
    expect(useAuthStore.getState().authRequired).toBe(false);
  });
});
