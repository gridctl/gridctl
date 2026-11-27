import type { IssueSeverity, ValidationIssue } from '../types';

export type YamlModule = typeof import('yaml');

export interface ResolvedIssue extends ValidationIssue {
  resolvedLine: number | null;
}

let yamlPromise: Promise<YamlModule> | null = null;
let yamlLoaded: YamlModule | null = null;

// The yaml package stays out of the Stack chunk: it is fetched on demand,
// once, and a failed fetch can be retried by a later call.
export function loadYaml(): Promise<YamlModule> {
  if (!yamlPromise) {
    yamlPromise = import('yaml').then(
      (mod) => {
        yamlLoaded = mod;
        return mod;
      },
      (err: unknown) => {
        yamlPromise = null;
        throw err;
      },
    );
  }
  return yamlPromise;
}

export function getLoadedYaml(): YamlModule | null {
  return yamlLoaded;
}

// Splits a validation field path into segments. Bracket contents are one
// verbatim segment (map keys may contain dots); an unclosed bracket ends the path.
export function tokenizeFieldPath(field: string): string[] {
  const segments: string[] = [];
  let current = '';
  let i = 0;
  while (i < field.length) {
    const ch = field[i];
    if (ch === '.') {
      if (current) segments.push(current);
      current = '';
      i++;
    } else if (ch === '[') {
      if (current) segments.push(current);
      current = '';
      const end = field.indexOf(']', i + 1);
      if (end === -1) return segments;
      segments.push(field.slice(i + 1, end));
      i = end + 1;
    } else {
      current += ch;
      i++;
    }
  }
  if (current) segments.push(current);
  return segments;
}

function lineCount(content: string): number {
  return content.split('\n').length;
}

function inRange(line: number | null | undefined, count: number): line is number {
  return typeof line === 'number' && Number.isInteger(line) && line >= 1 && line <= count;
}

function messageLine(issue: ValidationIssue): number | null {
  if (issue.field !== 'yaml') return null;
  const match = /\bline (\d+)\b/.exec(issue.message);
  return match ? Number(match[1]) : null;
}

function hasServerLine(issue: ValidationIssue): boolean {
  return typeof issue.line === 'number' && Number.isInteger(issue.line) && issue.line > 0;
}

// True when at least one issue can only be located through its field path.
// A `yaml` field is a syntax error, so its document cannot be walked.
export function needsFieldResolution(issues: ValidationIssue[]): boolean {
  return issues.some((issue) => !hasServerLine(issue) && issue.field !== 'yaml' && issue.field !== '');
}

interface ParsedDoc {
  content: string;
  root: unknown;
  linePos: (offset: number) => number;
}

let parsedCache: ParsedDoc | null = null;

// One parse per distinct content. A document with syntax errors yields no
// root, so field paths stay unresolved.
function parse(yaml: YamlModule, content: string): ParsedDoc {
  if (parsedCache && parsedCache.content === content) return parsedCache;
  const lineCounter = new yaml.LineCounter();
  let root: unknown = null;
  try {
    const doc = yaml.parseDocument(content, { lineCounter });
    root = doc.errors.length > 0 ? null : doc.contents;
  } catch {
    root = null;
  }
  parsedCache = { content, root, linePos: (offset) => lineCounter.linePos(offset).line };
  return parsedCache;
}

function nodeStart(node: unknown): number | null {
  const range = (node as { range?: [number, number, number] } | null)?.range;
  return range ? range[0] : null;
}

function scalarString(yaml: YamlModule, node: unknown): string | null {
  return yaml.isScalar(node) ? String(node.value) : null;
}

function resolveFieldLine(yaml: YamlModule, doc: ParsedDoc, field: string): number | null {
  let node: unknown = doc.root;
  let line: number | null = null;
  for (const segment of tokenizeFieldPath(field)) {
    if (yaml.isMap(node)) {
      const pair = node.items.find((p) => scalarString(yaml, p.key) === segment);
      const start = pair ? nodeStart(pair.key) : null;
      if (!pair || start === null) break;
      line = doc.linePos(start);
      node = pair.value;
    } else if (yaml.isSeq(node)) {
      let item: unknown;
      if (/^\d+$/.test(segment)) {
        item = node.items[Number(segment)];
      } else {
        item = node.items.find((it) => {
          if (!yaml.isMap(it)) return false;
          return it.items.some((p) => {
            const key = scalarString(yaml, p.key);
            return (key === 'id' || key === 'name') && scalarString(yaml, p.value) === segment;
          });
        });
      }
      const start = item ? nodeStart(item) : null;
      if (!item || start === null) break;
      line = doc.linePos(start);
      node = item;
    } else {
      break;
    }
  }
  return line;
}

// Locates each issue in the displayed content: the server line, then the
// node its field path addresses (only once yaml is loaded), then a line in a
// syntax error's message. A line outside the content is unknown.
export function resolveIssueLines(
  content: string,
  issues: ValidationIssue[],
  yaml: YamlModule | null,
): ResolvedIssue[] {
  const count = lineCount(content);
  const doc = yaml && needsFieldResolution(issues) ? parse(yaml, content) : null;
  return issues.map((issue) => {
    let line: number | null = null;
    if (hasServerLine(issue)) {
      line = issue.line ?? null;
    } else if (doc && yaml && issue.field !== 'yaml') {
      line = resolveFieldLine(yaml, doc, issue.field);
    }
    if (line === null) line = messageLine(issue);
    return { ...issue, resolvedLine: inRange(line, count) ? line : null };
  });
}

const SEVERITY_RANK: Record<IssueSeverity, number> = { error: 0, warning: 1, info: 2 };

// Errors, then warnings, then info; within a severity by line with unlocated
// issues last. Array sort is stable, so ties keep the server order.
export function sortIssues(issues: ResolvedIssue[]): ResolvedIssue[] {
  return [...issues].sort((a, b) => {
    const bySeverity = SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity];
    if (bySeverity !== 0) return bySeverity;
    const al = a.resolvedLine ?? Number.POSITIVE_INFINITY;
    const bl = b.resolvedLine ?? Number.POSITIVE_INFINITY;
    return al === bl ? 0 : al - bl;
  });
}

export function groupIssuesByLine(issues: ResolvedIssue[]): Map<number, ResolvedIssue[]> {
  const byLine = new Map<number, ResolvedIssue[]>();
  for (const issue of issues) {
    if (issue.resolvedLine === null) continue;
    const list = byLine.get(issue.resolvedLine);
    if (list) list.push(issue);
    else byLine.set(issue.resolvedLine, [issue]);
  }
  return byLine;
}

export function highestSeverity(issues: ValidationIssue[]): IssueSeverity | null {
  if (issues.length === 0) return null;
  if (issues.some((i) => i.severity === 'error')) return 'error';
  if (issues.some((i) => i.severity === 'warning')) return 'warning';
  return 'info';
}
