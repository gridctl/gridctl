export interface HighlightedToken {
  text: string;
  className: string;
}

export type Line = HighlightedToken[];

export type CodeLanguage = 'json' | 'yaml' | 'plain';

export function tokenize(content: string, language: CodeLanguage): Line[] {
  if (language === 'json') {
    const jsonLines = tokenizeJSON(content);
    if (jsonLines) return jsonLines;
    return tokenizePlain(content);
  }
  if (language === 'yaml') {
    return content.split('\n').map((line) => highlightYAMLLine(line));
  }
  return tokenizePlain(content);
}

export function tokenizePlain(content: string): Line[] {
  return content.split('\n').map((line) => [
    { text: line, className: 'text-text-primary' },
  ]);
}

// Walk a parsed JSON value and emit a styled pretty-printed token stream.
// Returns null on parse failure so the caller can fall back to plain text.
export function tokenizeJSON(content: string): Line[] | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(content);
  } catch {
    return null;
  }

  const lines: Line[] = [];
  let current: HighlightedToken[] = [];

  const push = (text: string, className: string) => {
    if (text.length > 0) current.push({ text, className });
  };
  const newline = () => {
    lines.push(current);
    current = [];
  };
  const indent = (depth: number) => {
    if (depth > 0) push('  '.repeat(depth), '');
  };

  const emit = (value: unknown, depth: number) => {
    if (value === null) {
      push('null', 'text-tertiary-light');
      return;
    }
    if (typeof value === 'boolean') {
      push(String(value), 'text-tertiary-light');
      return;
    }
    if (typeof value === 'number') {
      push(Number.isFinite(value) ? String(value) : 'null', 'text-primary');
      return;
    }
    if (typeof value === 'string') {
      push(JSON.stringify(value), 'text-primary-light');
      return;
    }
    if (Array.isArray(value)) {
      if (value.length === 0) {
        push('[]', 'text-text-muted');
        return;
      }
      push('[', 'text-text-muted');
      newline();
      value.forEach((item, idx) => {
        indent(depth + 1);
        emit(item, depth + 1);
        if (idx < value.length - 1) push(',', 'text-text-muted');
        newline();
      });
      indent(depth);
      push(']', 'text-text-muted');
      return;
    }
    if (typeof value === 'object') {
      const obj = value as Record<string, unknown>;
      const keys = Object.keys(obj);
      if (keys.length === 0) {
        push('{}', 'text-text-muted');
        return;
      }
      push('{', 'text-text-muted');
      newline();
      keys.forEach((key, idx) => {
        indent(depth + 1);
        push(JSON.stringify(key), 'text-secondary-light');
        push(': ', 'text-text-muted');
        emit(obj[key], depth + 1);
        if (idx < keys.length - 1) push(',', 'text-text-muted');
        newline();
      });
      indent(depth);
      push('}', 'text-text-muted');
      return;
    }
    push(String(value), 'text-text-primary');
  };

  emit(parsed, 0);
  newline();
  if (lines.length === 0) lines.push([]);
  return lines;
}

// TODO: dedupe with SpecTab.highlightYAMLLine — kept duplicated to avoid
// SpecTab regression risk (per PR 2 architecture guidance).
function highlightYAMLLine(line: string): HighlightedToken[] {
  const tokens: HighlightedToken[] = [];

  if (line.trimStart().startsWith('#')) {
    return [{ text: line, className: 'text-text-muted/60 italic' }];
  }

  const kvMatch = line.match(/^(\s*)([\w.-]+)(:)(.*)/);
  if (kvMatch) {
    const [, indent, key, colon, rest] = kvMatch;
    if (indent) tokens.push({ text: indent, className: '' });
    tokens.push({ text: key, className: 'text-secondary-light' });
    tokens.push({ text: colon, className: 'text-text-muted' });

    if (rest.trim()) {
      const value = rest;
      if (value.trim().startsWith('"') || value.trim().startsWith("'")) {
        tokens.push({ text: value, className: 'text-primary-light' });
      } else if (/^\s*(true|false|null|~)$/i.test(value)) {
        tokens.push({ text: value, className: 'text-tertiary-light' });
      } else if (/^\s*-?\d+(\.\d+)?$/.test(value.trim())) {
        tokens.push({ text: value, className: 'text-primary' });
      } else if (value.includes('${vault:')) {
        tokens.push({ text: value, className: 'text-status-pending' });
      } else {
        tokens.push({ text: value, className: 'text-text-primary' });
      }
    }
    return tokens;
  }

  const listMatch = line.match(/^(\s*)(- )(.*)/);
  if (listMatch) {
    const [, indent, dash, rest] = listMatch;
    if (indent) tokens.push({ text: indent, className: '' });
    tokens.push({ text: dash, className: 'text-text-muted' });
    tokens.push({ text: rest, className: 'text-text-primary' });
    return tokens;
  }

  return [{ text: line, className: 'text-text-primary' }];
}
