// Pure summary of a tool's JSON Schema input for the Tools workspace
// parameters table. MCP servers ship malformed schemas, so nothing here throws.

export interface SchemaParameter {
  name: string;
  type: string;
  required: boolean;
  description?: string;
  defaultText?: string;
  enumValues?: string[];
}

export interface SchemaParameters {
  rows: SchemaParameter[];
  notes: string[];
  empty: boolean;
}

type PlainObject = Record<string, unknown>;

function isPlainObject(value: unknown): value is PlainObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function stringify(value: unknown): string {
  try {
    const text = JSON.stringify(value);
    return text === undefined ? String(value) : text;
  } catch {
    return String(value);
  }
}

function refName(ref: string): string {
  const parts = ref.split('/');
  return parts[parts.length - 1] || ref;
}

function schemaType(schema: unknown, depth = 0): string {
  if (!isPlainObject(schema)) return 'any';
  const { type } = schema;
  if (type === 'array' && isPlainObject(schema.items) && typeof schema.items.type === 'string') {
    return `array<${schema.items.type}>`;
  }
  if (typeof type === 'string') return type;
  if (Array.isArray(type)) {
    const members = type.filter((t): t is string => typeof t === 'string');
    if (members.length > 0) return members.join(' | ');
  }
  if (type === undefined && 'enum' in schema) return 'enum';
  if (type === undefined && 'const' in schema) return 'const';
  const union = Array.isArray(schema.anyOf) ? schema.anyOf : Array.isArray(schema.oneOf) ? schema.oneOf : null;
  if (union && depth === 0) {
    const members = [...new Set(union.map((m) => schemaType(m, depth + 1)))];
    if (members.length > 0) return members.join(' | ');
  }
  if (typeof schema.$ref === 'string') return refName(schema.$ref);
  return 'any';
}

export function schemaParameters(schema: unknown): SchemaParameters {
  if (!isPlainObject(schema)) return { rows: [], notes: [], empty: false };

  const notes: string[] = [];
  const combinators = ['anyOf', 'oneOf', 'allOf', '$ref'].filter((key) => key in schema);
  if (combinators.length > 0) {
    notes.push(`The schema uses ${combinators.join(', ')} at the top level; see the raw schema for the full shape.`);
  }
  const extra = schema.additionalProperties;
  if (extra === true || isPlainObject(extra)) {
    notes.push('Accepts additional properties not listed here; see the raw schema.');
  }

  const properties = schema.properties;
  const required = Array.isArray(schema.required)
    ? new Set(schema.required.filter((r): r is string => typeof r === 'string'))
    : new Set<string>();

  const rows: SchemaParameter[] = [];
  let nested = false;
  if (isPlainObject(properties)) {
    for (const [name, prop] of Object.entries(properties)) {
      const row: SchemaParameter = { name, type: schemaType(prop), required: required.has(name) };
      if (isPlainObject(prop)) {
        if (typeof prop.description === 'string' && prop.description !== '') row.description = prop.description;
        if ('default' in prop) row.defaultText = stringify(prop.default);
        if (Array.isArray(prop.enum)) row.enumValues = prop.enum.map(stringify);
        if (isPlainObject(prop.properties) || typeof prop.$ref === 'string') nested = true;
      }
      rows.push(row);
    }
  }
  if (nested) {
    notes.push('Some parameters are nested objects or references; see the raw schema for their fields.');
  }

  const typeIsObject = schema.type === undefined || schema.type === 'object';
  const noProperties =
    properties === undefined || (isPlainObject(properties) && Object.keys(properties).length === 0);
  const empty = typeIsObject && noProperties && notes.length === 0;

  return { rows, notes, empty };
}
