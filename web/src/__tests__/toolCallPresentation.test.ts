import { describe, expect, it } from 'vitest';
import type { ToolCallEnvelope } from '../lib/api';
import {
  capSections,
  copyText,
  describeOutcome,
  formatElapsed,
  resultSections,
  sectionsLength,
  summarizeBlock,
} from '../lib/toolCallPresentation';

function env(overrides: Partial<ToolCallEnvelope>): ToolCallEnvelope {
  return {
    schema_version: 1,
    outcome: { disposition: 'completed', stage: 'downstream', reason: 'ok', completion: 'complete' },
    result: null,
    error: null,
    ...overrides,
  };
}

function denial(code: string, extra: Partial<ToolCallEnvelope['outcome']> = {}): ToolCallEnvelope {
  return env({
    outcome: { disposition: 'denied', stage: 'gate', reason: code, completion: 'not_started', ...extra },
    error: { code, message: `msg ${code}` },
  });
}

describe('describeOutcome', () => {
  it('classifies success', () => {
    expect(describeOutcome(env({ result: { content: [] } }))).toMatchObject({ kind: 'completed', label: 'Completed' });
  });

  it('classifies tool errors by isError or disposition', () => {
    expect(describeOutcome(env({ result: { isError: true } })).kind).toBe('tool_error');
    expect(
      describeOutcome(env({
        outcome: { disposition: 'tool_error', stage: 'downstream', reason: 'tool_error', completion: 'complete' },
        error: { code: 'tool_error', message: 'tool returned an error' },
      })),
    ).toMatchObject({ kind: 'tool_error', label: 'Tool returned an error' });
  });

  it('maps known codes to sentences', () => {
    expect(describeOutcome(denial('client_scope')).sentences[0]).toContain('Add a ui profile under clients:');
    expect(describeOutcome(denial('no_replica')).sentences).toEqual(['No healthy replica is available for this server.']);
    expect(describeOutcome(denial('cold_start')).sentences).toEqual(['The server failed to start.']);
    expect(describeOutcome(denial('gate_denied', { gate: 'rate_limit' })).sentences[0]).toContain('Gate: rate_limit.');
    const pin = describeOutcome(denial('schema_pin'));
    expect(pin.pinsLink).toBe(true);
    expect(pin.sentences[0]).toBe('The server is blocked until its schema change is approved.');
  });

  it('falls back to the message and code for unmapped codes', () => {
    const view = describeOutcome(denial('invalid_name'));
    expect(view).toMatchObject({ kind: 'not_run', label: 'Failed (invalid_name)', message: 'msg invalid_name', sentences: [] });
    expect(describeOutcome(denial('something_new')).label).toBe('Failed (something_new)');
  });

  it('adds uncertainty and execution-admission sentences', () => {
    const view = describeOutcome(denial('transport_error', { completion: 'unknown', detail: 'execution_admission' }));
    expect(view.sentences).toContain('The tool may or may not have run.');
    expect(view.sentences).toContain("The server's execution evidence refused the call.");
  });
});

describe('result sections', () => {
  it('escapes text, pretty-prints JSON, and summarizes non-text blocks', () => {
    const sections = resultSections({
      content: [
        { type: 'text', text: 'a\u202eb\nline' },
        { type: 'text', text: '{"x":1}' },
        { type: 'image', data: 'AAAA', mimeType: 'image/png' },
      ],
      structuredContent: { y: 2 },
    });
    expect(sections.map((s) => s.kind)).toEqual(['text', 'text', 'summary', 'structured']);
    expect(sections[0].text).toBe('a\\u202eb\nline');
    expect(sections[1].text).toBe('{\n  "x": 1\n}');
    expect(sections[2].text).toBe('[image content], image/png, about 3 B');
    expect(sections[3].text).toContain('"y": 2');
  });

  it('summarizes embedded resources without their payload', () => {
    expect(summarizeBlock({ type: 'resource', resource: { mimeType: 'text/plain', text: 'secret' } })).toBe(
      '[resource content], text/plain, about 6 B',
    );
  });

  it('caps combined text', () => {
    const sections = resultSections({ content: [{ type: 'text', text: 'x'.repeat(15) }, { type: 'text', text: 'y'.repeat(15) }] });
    const capped = capSections(sections, 20);
    expect(sectionsLength(capped)).toBe(20);
    expect(capped[1].text).toBe('yyyyy');
  });

  it('copies joined text or the full JSON', () => {
    expect(copyText({ content: [{ type: 'text', text: 'a' }, { type: 'text', text: 'b' }] })).toBe('a\n\nb');
    const withImage = { content: [{ type: 'image', data: 'AA' }] };
    expect(JSON.parse(copyText(withImage))).toEqual(withImage);
    expect(copyText(null)).toBe('');
  });

  it('formats elapsed time', () => {
    expect(formatElapsed(42)).toBe('42 ms');
    expect(formatElapsed(1500)).toBe('1.5 s');
  });
});
