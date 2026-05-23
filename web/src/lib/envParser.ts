import type { VariableType } from './api';

export interface ParsedEnvEntry {
  // 1-based source line for surfacing parse errors next to the textarea.
  line: number;
  key: string;
  value: string;
  // Auto-detected from the value shape — overridable in the import preview.
  type: VariableType;
  // True when the source value was wrapped in single or double quotes. The UI
  // uses this to render the literal value rather than stripping quotes.
  quoted: boolean;
}

export interface IgnoredEnvLine {
  line: number;
  raw: string;
  reason: string;
}

export interface ParseEnvResult {
  entries: ParsedEnvEntry[];
  ignored: IgnoredEnvLine[];
}

const KEY_REGEX = /^[A-Za-z_][A-Za-z0-9_]*$/;

// parseEnv parses a textual `.env`-style block into a list of key/value
// entries plus any lines we couldn't make sense of. Behaviour is intentionally
// strict-but-tolerant: a bad line never aborts parsing of subsequent lines.
//
// Supported:
//   KEY=value
//   KEY="quoted with spaces and ="
//   KEY='single-quoted'
//   KEY=value # trailing comment (only outside quotes)
//   export KEY=value
//   # comment line
//   <blank line>
//
// Not supported (logged as ignored):
//   Multi-line values that span source lines.
//   Variable interpolation like KEY=$OTHER.
//   Lowercase or symbol-leading keys.
export function parseEnv(input: string): ParseEnvResult {
  const entries: ParsedEnvEntry[] = [];
  const ignored: IgnoredEnvLine[] = [];

  const lines = input.split(/\r?\n/);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const lineNo = i + 1;
    const trimmed = line.trim();
    if (trimmed.length === 0) continue;
    if (trimmed.startsWith('#')) continue;

    // Strip an optional leading `export ` so values copied from shell
    // scripts work without modification.
    const stripped = trimmed.startsWith('export ')
      ? trimmed.slice('export '.length).trimStart()
      : trimmed;

    const eqIdx = stripped.indexOf('=');
    if (eqIdx <= 0) {
      ignored.push({
        line: lineNo,
        raw: line,
        reason: 'missing = separator',
      });
      continue;
    }

    const rawKey = stripped.slice(0, eqIdx).trim();
    if (!KEY_REGEX.test(rawKey)) {
      ignored.push({
        line: lineNo,
        raw: line,
        reason: `invalid key "${rawKey}"`,
      });
      continue;
    }

    const rawValue = stripped.slice(eqIdx + 1);
    const parsed = parseValue(rawValue);
    if (!parsed.ok) {
      ignored.push({ line: lineNo, raw: line, reason: parsed.reason });
      continue;
    }

    entries.push({
      line: lineNo,
      key: rawKey,
      value: parsed.value,
      type: detectType(parsed.value, parsed.quoted),
      quoted: parsed.quoted,
    });
  }

  return { entries, ignored };
}

interface ParseValueOk {
  ok: true;
  value: string;
  quoted: boolean;
}
interface ParseValueErr {
  ok: false;
  reason: string;
}

function parseValue(raw: string): ParseValueOk | ParseValueErr {
  const trimmed = raw.trim();
  if (trimmed.length === 0) {
    return { ok: true, value: '', quoted: false };
  }

  const first = trimmed[0];
  if (first === '"' || first === "'") {
    // Find the matching closing quote, allowing the same quote to be escaped
    // with a backslash inside a double-quoted string.
    const quote = first;
    let i = 1;
    let value = '';
    while (i < trimmed.length) {
      const ch = trimmed[i];
      if (ch === '\\' && quote === '"' && i + 1 < trimmed.length) {
        const next = trimmed[i + 1];
        if (next === 'n') value += '\n';
        else if (next === 't') value += '\t';
        else if (next === 'r') value += '\r';
        else value += next;
        i += 2;
        continue;
      }
      if (ch === quote) {
        // Allow a trailing comment after the closing quote.
        const tail = trimmed.slice(i + 1).trim();
        if (tail.length > 0 && !tail.startsWith('#')) {
          return {
            ok: false,
            reason: 'unexpected content after closing quote',
          };
        }
        return { ok: true, value, quoted: true };
      }
      value += ch;
      i++;
    }
    return { ok: false, reason: 'unterminated quoted value' };
  }

  // Unquoted: strip a trailing comment (anything after the first unquoted #
  // preceded by whitespace), then trim trailing whitespace.
  const hashIdx = findInlineComment(trimmed);
  const before = hashIdx >= 0 ? trimmed.slice(0, hashIdx) : trimmed;
  return { ok: true, value: before.trimEnd(), quoted: false };
}

function findInlineComment(s: string): number {
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '#' && (i === 0 || /\s/.test(s[i - 1]))) return i;
  }
  return -1;
}

function detectType(value: string, quoted: boolean): VariableType {
  if (quoted) return 'string';
  const trimmed = value.trim();
  if (trimmed === '') return 'string';
  if (trimmed === 'true' || trimmed === 'false') return 'bool';
  if (/^-?\d+(\.\d+)?$/.test(trimmed)) return 'number';
  if (
    (trimmed.startsWith('{') && trimmed.endsWith('}')) ||
    (trimmed.startsWith('[') && trimmed.endsWith(']'))
  ) {
    try {
      JSON.parse(trimmed);
      return 'json';
    } catch {
      return 'string';
    }
  }
  return 'string';
}
