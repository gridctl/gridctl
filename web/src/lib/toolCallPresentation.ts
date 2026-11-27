// Presentation helpers for POST /api/tools/call envelopes in the Tools
// workspace "Try it" section. Tool output is attacker-controlled: everything
// here produces plain strings that the UI renders as text only.
import type { ToolCallContent, ToolCallEnvelope } from './api';
import { escapeNonPrintable } from './nonPrintable';

export const RESULT_CHAR_CAP = 20_000;

export type OutcomeClass = 'completed' | 'tool_error' | 'not_run';

export interface OutcomeView {
  kind: OutcomeClass;
  label: string;
  /** error.message for anything other than success. */
  message?: string;
  /** Mapped explanation lines, in display order. */
  sentences: string[];
  /** True when the explanation should link to the Pins workspace. */
  pinsLink: boolean;
}

const CODE_LABEL: Record<string, string> = {
  client_scope: 'Outside client scope',
  policy_blocked: 'Blocked by policy',
  gate_denied: 'Denied by a call gate',
  schema_pin: 'Blocked by schema pin',
  unknown_tool: 'Unknown tool',
  no_replica: 'No replica available',
  cold_start: 'Cold start failed',
  transport_error: 'Transport error',
  timeout: 'Timed out',
  cancelled: 'Cancelled by the gateway',
  input_required: 'Input required',
};

const CODE_SENTENCE: Record<string, string> = {
  client_scope: 'The ui caller is outside the client scope. Add a ui profile under clients: or use gridctl call --as <client>.',
  policy_blocked: 'A policy: rule blocks this tool.',
  schema_pin: 'The server is blocked until its schema change is approved.',
  unknown_tool: 'The gateway does not expose this tool. It is hidden by the allow-list or no longer exists.',
  no_replica: 'No healthy replica is available for this server.',
  cold_start: 'The server failed to start.',
  transport_error: 'The connection to the server failed during the call.',
  timeout: 'The call timed out.',
  cancelled: 'The gateway cancelled the call.',
  input_required: 'The tool asked for more input. Replying is not supported here.',
};

/** Classifies an envelope and builds its outcome line and explanation. */
export function describeOutcome(env: ToolCallEnvelope): OutcomeView {
  const { outcome, error, result } = env;
  if (outcome.reason === 'ok' && !result?.isError) {
    return { kind: 'completed', label: 'Completed', sentences: [], pinsLink: false };
  }
  if (result?.isError || outcome.disposition === 'tool_error') {
    return {
      kind: 'tool_error',
      label: 'Tool returned an error',
      message: error?.message,
      sentences: [],
      pinsLink: false,
    };
  }
  const code = error?.code ?? outcome.reason;
  const sentences: string[] = [];
  if (code === 'gate_denied') {
    sentences.push(
      `A call gate denied the call (a rate limit or another gate).${outcome.gate ? ` Gate: ${outcome.gate}.` : ''}`,
    );
  } else if (CODE_SENTENCE[code]) {
    sentences.push(CODE_SENTENCE[code]);
  }
  if (outcome.detail === 'execution_admission') {
    sentences.push("The server's execution evidence refused the call.");
  }
  if (outcome.completion === 'unknown') sentences.push('The tool may or may not have run.');
  return {
    kind: 'not_run',
    label: CODE_LABEL[code] ?? `Failed (${code})`,
    message: error?.message,
    sentences,
    pinsLink: code === 'schema_pin',
  };
}

/** The outcome line announced to assistive technology and shown above the result. */
export function outcomeLine(env: ToolCallEnvelope, elapsedMs: number): string {
  const view = describeOutcome(env);
  const parts = [view.label, formatElapsed(elapsedMs)];
  if (view.kind !== 'completed' && view.message) parts.push(view.message);
  return parts.join(' · ');
}

/** Elapsed time for the outcome line. */
export function formatElapsed(ms: number): string {
  if (ms < 1000) return `${Math.max(0, Math.round(ms))} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

// Escapes non-printable characters line by line so line breaks still render
// as line breaks while bidi, zero-width, and control characters show up.
export function escapeForDisplay(text: string): string {
  return text.split('\n').map(escapeNonPrintable).join('\n');
}

function approxSize(chars: number): string {
  if (chars < 1024) return `${chars} B`;
  if (chars < 1024 * 1024) return `${(chars / 1024).toFixed(1)} KB`;
  return `${(chars / (1024 * 1024)).toFixed(1)} MB`;
}

/** One line describing a non-text content block, never its payload. */
export function summarizeBlock(block: ToolCallContent): string {
  const type = typeof block.type === 'string' ? block.type : 'unknown';
  const parts = [`[${escapeNonPrintable(type)} content]`];
  const mime = typeof block.mimeType === 'string'
    ? block.mimeType
    : typeof (block.resource as { mimeType?: unknown } | undefined)?.mimeType === 'string'
      ? (block.resource as { mimeType: string }).mimeType
      : undefined;
  if (mime) parts.push(escapeNonPrintable(mime));
  const resource = block.resource as { blob?: unknown; text?: unknown } | undefined;
  const base64 = typeof block.data === 'string' ? block.data : typeof resource?.blob === 'string' ? resource.blob : undefined;
  const text = typeof resource?.text === 'string' ? resource.text : undefined;
  if (base64 !== undefined) parts.push(`about ${approxSize(Math.floor((base64.length * 3) / 4))}`);
  else if (text !== undefined) parts.push(`about ${approxSize(text.length)}`);
  return parts.join(', ');
}

function prettyIfJSON(text: string): string {
  const t = text.trim();
  if (!(t.startsWith('{') || t.startsWith('['))) return text;
  try {
    return JSON.stringify(JSON.parse(t), null, 2);
  } catch {
    return text;
  }
}

export interface ResultSection {
  kind: 'text' | 'summary' | 'structured';
  label?: string;
  text: string;
}

/** Display sections for a result, escaped and pretty-printed but not capped. */
export function resultSections(result: ToolCallEnvelope['result']): ResultSection[] {
  if (!result) return [];
  const sections: ResultSection[] = [];
  const content = Array.isArray(result.content) ? result.content : [];
  for (const block of content) {
    if (block && block.type === 'text' && typeof block.text === 'string') {
      sections.push({ kind: 'text', text: escapeForDisplay(prettyIfJSON(block.text)) });
    } else if (block && typeof block === 'object') {
      sections.push({ kind: 'summary', text: summarizeBlock(block) });
    }
  }
  if (result.structuredContent !== undefined && result.structuredContent !== null) {
    sections.push({
      kind: 'structured',
      label: 'Structured content',
      text: escapeForDisplay(JSON.stringify(result.structuredContent, null, 2) ?? ''),
    });
  }
  return sections;
}

/** Total characters across sections. */
export function sectionsLength(sections: ResultSection[]): number {
  return sections.reduce((n, s) => n + s.text.length, 0);
}

/**
 * Caps the combined text of sections at `cap` characters, slicing before the
 * strings reach React so an oversized result never mounts in full.
 */
export function capSections(sections: ResultSection[], cap = RESULT_CHAR_CAP): ResultSection[] {
  const out: ResultSection[] = [];
  let left = cap;
  for (const s of sections) {
    if (left <= 0) break;
    if (s.text.length <= left) {
      out.push(s);
      left -= s.text.length;
    } else {
      out.push({ ...s, text: s.text.slice(0, left) });
      left = 0;
    }
  }
  return out;
}

/**
 * The full, uncapped text Copy places on the clipboard: text blocks joined
 * by blank lines, or the whole result as JSON when it has non-text blocks or
 * structured content.
 */
export function copyText(result: ToolCallEnvelope['result']): string {
  if (!result) return '';
  const content = Array.isArray(result.content) ? result.content : [];
  const allText = content.every((b) => b && b.type === 'text' && typeof b.text === 'string');
  const structured = result.structuredContent !== undefined && result.structuredContent !== null;
  if (!allText || structured) return JSON.stringify(result, null, 2);
  return content.map((b) => b.text as string).join('\n\n');
}
