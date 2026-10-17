import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import * as api from '../lib/api';
import { cardPrefill, useDetectCard } from '../hooks/useDetectCard';

describe('useDetectCard', () => {
  beforeEach(() => {
    vi.spyOn(api, 'detectServerCard');
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('starts idle', () => {
    const { result } = renderHook(() => useDetectCard());
    expect(result.current.loading).toBe(false);
    expect(result.current.result).toBeNull();
    expect(result.current.error).toBeNull();
  });

  it('stores the draft and never a raw credential', async () => {
    vi.mocked(api.detectServerCard).mockResolvedValueOnce({
      card: { name: 'io.example/lab' },
      server: {
        name: 'lab',
        url: 'https://example.test/mcp',
        transport: 'http',
        auth: { type: 'bearer', token: '${var:TOKEN}' },
      },
      inputs: [{ name: 'token', required: true, secret: true }],
    });
    const { result } = renderHook(() => useDetectCard());
    await act(async () => {
      await result.current.detect('https://example.test/mcp');
    });
    expect(result.current.result?.server.auth?.token).toBe('${var:TOKEN}');
    expect(result.current.result?.inputs?.[0].name).toBe('token');
    expect(result.current.error).toBeNull();
  });
});

describe('cardPrefill', () => {
  it('prefills transport, URL, and a placeholder', () => {
    const patch = cardPrefill({
      name: 'lab',
      url: 'https://example.test/mcp',
      transport: 'http',
      auth: { type: 'bearer', token: '${var:TOKEN}' },
    });
    expect(patch).toEqual({
      url: 'https://example.test/mcp',
      transport: 'http',
      auth: { type: 'bearer', token: '${var:TOKEN}' },
    });
  });

  it('drops a literal credential', () => {
    const patch = cardPrefill({
      name: 'lab',
      url: 'https://example.test/mcp',
      transport: 'sse',
      auth: { type: 'bearer', token: 'literal-secret' },
    });
    expect(patch.auth?.token).toBeUndefined();
  });
});
