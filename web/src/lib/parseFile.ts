import type { VariableType } from './api';
import {
  parseEnv,
  isValidKey,
  type ParseEnvResult,
  type ParsedEnvEntry,
  type IgnoredEnvLine,
} from './envParser';

// parseFile turns dropped/pasted/picked import content into the same
// {entries, ignored} shape the import preview already consumes, regardless of
// whether the source is a `.env` block or a JSON document. Detection is by
// content, not filename, so pasted JSON works the same as a dropped `.json`.

const VALID_TYPES = new Set<VariableType>([
  'string',
  'json',
  'list',
  'number',
  'bool',
]);

// looksLikeJson is intentionally narrow: both supported JSON shapes are
// objects, and a `.env` block never begins with `{`, so a leading brace is an
// unambiguous signal.
function looksLikeJson(input: string): boolean {
  return input.trimStart().startsWith('{');
}

// parseImport dispatches to the JSON or env parser based on content.
export function parseImport(input: string): ParseEnvResult {
  return looksLikeJson(input) ? parseVariablesJson(input) : parseEnv(input);
}

// parseVariablesJson follows the CLI's parseVariablesJSON (cmd/gridctl/var.go):
// it accepts either the v2 `{ "variables": [...] }` shape (carrying explicit
// type / is_secret / set) or the legacy `{ "KEY": "value" }` map (everything
// string + secret). It differs deliberately in one place — an absent is_secret
// defaults to secret here (Article XII) rather than the CLI's zero-value false.
// Unparseable input returns a single ignored entry so the modal surfaces it the
// same way it surfaces bad `.env` lines.
export function parseVariablesJson(input: string): ParseEnvResult {
  const trimmed = input.trim();
  if (trimmed === '') return { entries: [], ignored: [] };

  let data: unknown;
  try {
    data = JSON.parse(trimmed);
  } catch {
    return { entries: [], ignored: [ignored('invalid JSON', trimmed)] };
  }

  if (!isRecord(data)) {
    return { entries: [], ignored: [ignored('expected a JSON object', trimmed)] };
  }

  // v2 shape wins when a `variables` array is present, matching the CLI.
  if (Array.isArray((data as Record<string, unknown>).variables)) {
    return fromV2((data as Record<string, unknown>).variables as unknown[]);
  }

  return fromLegacyMap(data as Record<string, unknown>);
}

function fromV2(items: unknown[]): ParseEnvResult {
  const entries: ParsedEnvEntry[] = [];
  const ignoredLines: IgnoredEnvLine[] = [];

  items.forEach((item, idx) => {
    const line = idx + 1;
    if (!isRecord(item)) {
      ignoredLines.push({ line, raw: snippet(String(item)), reason: 'not an object' });
      return;
    }
    const key = typeof item.key === 'string' ? item.key : '';
    if (!isValidKey(key)) {
      ignoredLines.push({ line, raw: snippet(key || JSON.stringify(item)), reason: `invalid key "${key}"` });
      return;
    }
    const type =
      typeof item.type === 'string' && VALID_TYPES.has(item.type as VariableType)
        ? (item.type as VariableType)
        : 'string';
    // Honor an explicit secret flag (snake_case is the canonical export field;
    // accept camelCase too), defaulting to secret per Article XII.
    const secretRaw = item.is_secret ?? item.isSecret;
    const isSecret = typeof secretRaw === 'boolean' ? secretRaw : true;
    entries.push({
      line,
      key,
      value: coerceValue(item.value),
      type,
      quoted: false,
      isSecret,
      set: typeof item.set === 'string' && item.set ? item.set : undefined,
    });
  });

  return { entries, ignored: ignoredLines };
}

function fromLegacyMap(map: Record<string, unknown>): ParseEnvResult {
  const entries: ParsedEnvEntry[] = [];
  const ignoredLines: IgnoredEnvLine[] = [];

  Object.entries(map).forEach(([key, value], idx) => {
    const line = idx + 1;
    if (!isValidKey(key)) {
      ignoredLines.push({ line, raw: snippet(key), reason: `invalid key "${key}"` });
      return;
    }
    // Legacy maps carry no metadata — everything imports as a string secret,
    // exactly like the CLI's legacy branch.
    entries.push({
      line,
      key,
      value: coerceValue(value),
      type: 'string',
      quoted: false,
      isSecret: true,
    });
  });

  return { entries, ignored: ignoredLines };
}

// isImportableFile gates which dropped files we try to read. `accept` does not
// apply to drops, so the type check happens here in JS.
export function isImportableFile(file: File): boolean {
  const name = file.name.toLowerCase();
  // Named env/json/text files, including dotfiles like `.env.local`.
  if (/\.(env|json|txt)$/.test(name) || name.startsWith('.env.')) {
    return true;
  }
  // Otherwise fall back to MIME: text and JSON, plus the empty type browsers
  // report for many extensionless config files.
  const type = file.type;
  return type === '' || type === 'application/json' || type.startsWith('text/');
}

function coerceValue(value: unknown): string {
  if (typeof value === 'string') return value;
  if (value === null || value === undefined) return '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function snippet(raw: string): string {
  return raw.length > 60 ? `${raw.slice(0, 60)}…` : raw;
}

function ignored(reason: string, raw: string): IgnoredEnvLine {
  return { line: 1, raw: snippet(raw), reason };
}
