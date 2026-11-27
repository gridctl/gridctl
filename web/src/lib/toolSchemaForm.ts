// Pure helpers behind the Tools workspace "Try it" form: derive form fields
// from a tool's input schema, convert between form values and the argument
// object, and validate arguments against a documented JSON Schema subset.
// Tool schemas come from downstream servers and may be malformed; nothing
// here throws on unexpected shapes.

export type FieldKind = 'enum' | 'string' | 'number' | 'integer' | 'boolean' | 'json';

export interface FormField {
  name: string;
  kind: FieldKind;
  required: boolean;
  typeLabel: string;
  description?: string;
  enumValues?: unknown[];
  hasDefault: boolean;
  default?: unknown;
}

export interface FormModel {
  /** False when the schema cannot drive a form; the UI offers JSON only. */
  formable: boolean;
  fields: FormField[];
}

/** Form state: one string per set field; a missing key means unset. */
export type FieldValues = Record<string, string | undefined>;

export interface ValidationResult {
  /** Error messages keyed by JSON Pointer ('' is the argument object). */
  errors: Record<string, string>;
  /** True when part of the schema uses keywords this validator does not check. */
  uncheckedKeywords: boolean;
}

const UNSUPPORTED_KEYWORDS = [
  '$ref', '$dynamicRef', '$recursiveRef', 'oneOf', 'anyOf', 'allOf', 'not', 'if', 'then', 'else',
  'patternProperties', 'dependentRequired', 'dependentSchemas', 'dependencies', 'format',
  'prefixItems', 'additionalItems', 'contains', 'minContains', 'maxContains', 'uniqueItems',
  'propertyNames', 'minProperties', 'maxProperties', 'multipleOf', 'unevaluatedProperties',
  'unevaluatedItems',
];

const COMPOSITE_KEYWORDS = ['$ref', 'oneOf', 'anyOf', 'allOf'];
const KNOWN_TYPES = new Set(['string', 'number', 'integer', 'boolean', 'object', 'array', 'null']);

type Schema = Record<string, unknown>;

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function isPrimitive(v: unknown): boolean {
  return v === null || ['string', 'number', 'boolean'].includes(typeof v);
}

function requiredList(schema: Schema): string[] {
  return Array.isArray(schema.required)
    ? schema.required.filter((r): r is string => typeof r === 'string')
    : [];
}

/** JSON with object keys sorted, for equality and de-duplication. */
export function canonicalJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalJSON).join(',')}]`;
  if (isPlainObject(value)) {
    const keys = Object.keys(value).sort();
    return `{${keys.map((k) => `${JSON.stringify(k)}:${canonicalJSON(value[k])}`).join(',')}}`;
  }
  return JSON.stringify(value) ?? 'null';
}

function escapePointerSegment(s: string): string {
  return s.replace(/~/g, '~0').replace(/\//g, '~1');
}

/** Appends one segment to a JSON Pointer. */
export function childPointer(parent: string, segment: string | number): string {
  return `${parent}/${escapePointerSegment(String(segment))}`;
}

/** The top-level property a pointer belongs to, or null for the root. */
export function topLevelName(pointer: string): string | null {
  if (!pointer.startsWith('/')) return null;
  const first = pointer.slice(1).split('/')[0];
  return first.replace(/~1/g, '/').replace(/~0/g, '~');
}

function fieldKind(prop: unknown): { kind: FieldKind; typeLabel: string } {
  if (!isPlainObject(prop)) return { kind: 'json', typeLabel: 'json' };
  const type = prop.type;
  const typeLabel = typeof type === 'string'
    ? type
    : Array.isArray(type)
      ? type.filter((t) => typeof t === 'string').join(' | ') || 'json'
      : 'json';
  if (COMPOSITE_KEYWORDS.some((k) => k in prop)) return { kind: 'json', typeLabel };
  if (Array.isArray(prop.enum) && prop.enum.length > 0 && prop.enum.every(isPrimitive)) {
    return { kind: 'enum', typeLabel: typeof type === 'string' ? type : 'enum' };
  }
  if (type === 'string' || type === 'number' || type === 'integer' || type === 'boolean') {
    return { kind: type, typeLabel };
  }
  return { kind: 'json', typeLabel };
}

/** Derives one form field per top-level property, in declared order. */
export function deriveFields(schema: unknown): FormModel {
  if (!isPlainObject(schema)) return { formable: false, fields: [] };
  if (schema.type !== undefined && schema.type !== 'object') return { formable: false, fields: [] };
  if (schema.properties === undefined) return { formable: true, fields: [] };
  if (!isPlainObject(schema.properties)) return { formable: false, fields: [] };
  const required = new Set(requiredList(schema));
  const fields = Object.entries(schema.properties).map(([name, prop]): FormField => {
    const { kind, typeLabel } = fieldKind(prop);
    const p = isPlainObject(prop) ? prop : {};
    return {
      name,
      kind,
      required: required.has(name),
      typeLabel,
      description: typeof p.description === 'string' ? p.description : undefined,
      enumValues: kind === 'enum' ? (p.enum as unknown[]) : undefined,
      hasDefault: 'default' in p,
      default: p.default,
    };
  });
  return { formable: true, fields };
}

// Finite JSON number syntax only: no hex, no Infinity, no trailing text.
const JSON_NUMBER = /^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$/;

/** Parses a JSON number literal; null when it is not a finite JSON number. */
export function parseJSONNumber(text: string): number | null {
  const t = text.trim();
  if (!JSON_NUMBER.test(t)) return null;
  const n = Number(t);
  return Number.isFinite(n) ? n : null;
}

/** Converts one argument value to the field's form value, or null when the control cannot hold it. */
export function fieldValueFromArg(field: FormField, value: unknown): string | null {
  switch (field.kind) {
    case 'string':
      return typeof value === 'string' ? value : null;
    case 'number':
    case 'integer':
      return typeof value === 'number' && Number.isFinite(value) ? String(value) : null;
    case 'boolean':
      return typeof value === 'boolean' ? String(value) : null;
    case 'enum': {
      const key = canonicalJSON(value);
      const idx = (field.enumValues ?? []).findIndex((v) => canonicalJSON(v) === key);
      return idx >= 0 ? String(idx) : null;
    }
    case 'json':
      return value === undefined ? null : JSON.stringify(value, null, 2);
  }
}

/**
 * Splits an argument object into form values and the arguments the form
 * cannot show (unknown properties, or values the field's control cannot
 * hold). The extras are preserved and sent unchanged.
 */
export function fieldValuesFromArgs(
  model: FormModel,
  args: Record<string, unknown>,
): { values: FieldValues; extra: Record<string, unknown> } {
  const values: FieldValues = {};
  const extra: Record<string, unknown> = {};
  const byName = new Map(model.fields.map((f) => [f.name, f]));
  for (const [key, value] of Object.entries(args)) {
    const field = byName.get(key);
    const converted = field ? fieldValueFromArg(field, value) : null;
    if (field && converted !== null) values[key] = converted;
    else extra[key] = value;
  }
  return { values, extra };
}

/**
 * Builds the argument object from form values plus preserved extras. Unset
 * fields are omitted, and so is an empty optional string or number. Values
 * that do not parse are reported by pointer instead of being sent.
 */
export function argsFromFieldValues(
  model: FormModel,
  values: FieldValues,
  extra: Record<string, unknown> = {},
): { args: Record<string, unknown>; errors: Record<string, string> } {
  const args: Record<string, unknown> = { ...extra };
  const errors: Record<string, string> = {};
  for (const field of model.fields) {
    const raw = values[field.name];
    if (raw === undefined) continue;
    const pointer = childPointer('', field.name);
    switch (field.kind) {
      case 'string':
        if (raw === '' && !field.required) continue;
        args[field.name] = raw;
        break;
      case 'number':
      case 'integer': {
        if (raw.trim() === '') continue;
        const n = parseJSONNumber(raw);
        if (n === null) errors[pointer] = field.kind === 'integer' ? 'Must be an integer' : 'Must be a number';
        else args[field.name] = n;
        break;
      }
      case 'boolean':
        if (raw === 'true') args[field.name] = true;
        else if (raw === 'false') args[field.name] = false;
        break;
      case 'enum': {
        if (raw === '') continue;
        const values = field.enumValues ?? [];
        const idx = Number(raw);
        if (Number.isInteger(idx) && idx >= 0 && idx < values.length) args[field.name] = values[idx];
        break;
      }
      case 'json': {
        if (raw.trim() === '') continue;
        try {
          args[field.name] = JSON.parse(raw);
        } catch {
          errors[pointer] = 'Must be valid JSON';
        }
        break;
      }
    }
  }
  return { args, errors };
}

/** True when the schema, at any depth the validator walks, uses an unsupported keyword. */
export function hasUncheckedKeywords(schema: unknown): boolean {
  const seen = new Set<unknown>();
  const walk = (node: unknown): boolean => {
    if (!isPlainObject(node) || seen.has(node)) return false;
    seen.add(node);
    if (UNSUPPORTED_KEYWORDS.some((k) => k in node)) return true;
    if (node.type !== undefined) {
      const types = Array.isArray(node.type) ? node.type : [node.type];
      if (types.some((t) => typeof t !== 'string' || !KNOWN_TYPES.has(t))) return true;
    }
    if (typeof node.exclusiveMinimum === 'boolean' && typeof node.minimum !== 'number') return true;
    if (typeof node.exclusiveMaximum === 'boolean' && typeof node.maximum !== 'number') return true;
    if (typeof node.pattern === 'string' && compilePattern(node.pattern) === null) return true;
    if (Array.isArray(node.items)) return true;
    if (isPlainObject(node.additionalProperties)) return true;
    if (isPlainObject(node.properties) && Object.values(node.properties).some(walk)) return true;
    return walk(node.items);
  };
  return walk(schema);
}

function compilePattern(pattern: string): RegExp | null {
  try {
    return new RegExp(pattern, 'u');
  } catch {
    return null;
  }
}

function typeMatches(type: string, value: unknown): boolean {
  switch (type) {
    case 'string': return typeof value === 'string';
    case 'number': return typeof value === 'number' && Number.isFinite(value);
    case 'integer': return typeof value === 'number' && Number.isInteger(value);
    case 'boolean': return typeof value === 'boolean';
    case 'object': return isPlainObject(value);
    case 'array': return Array.isArray(value);
    case 'null': return value === null;
    default: return true;
  }
}

const TYPE_MESSAGE: Record<string, string> = {
  string: 'a string',
  number: 'a number',
  integer: 'an integer',
  boolean: 'true or false',
  object: 'an object',
  array: 'an array',
  null: 'null',
};

function preview(value: unknown): string {
  const text = canonicalJSON(value);
  return text.length > 40 ? `${text.slice(0, 40)}…` : text;
}

function validateNode(schema: unknown, value: unknown, pointer: string, errors: Record<string, string>): void {
  if (!isPlainObject(schema)) return;
  const fail = (path: string, message: string) => {
    if (!(path in errors)) errors[path] = message;
  };

  const types = typeof schema.type === 'string'
    ? [schema.type]
    : Array.isArray(schema.type) ? schema.type.filter((t): t is string => typeof t === 'string') : [];
  const known = types.filter((t) => KNOWN_TYPES.has(t));
  if (known.length > 0 && known.length === types.length && !known.some((t) => typeMatches(t, value))) {
    fail(pointer, `Must be ${known.map((t) => TYPE_MESSAGE[t]).join(' or ')}`);
    return;
  }

  if (Array.isArray(schema.enum) && !schema.enum.some((v) => canonicalJSON(v) === canonicalJSON(value))) {
    fail(pointer, `Must be one of: ${schema.enum.slice(0, 8).map(preview).join(', ')}${schema.enum.length > 8 ? ', …' : ''}`);
    return;
  }
  if ('const' in schema && canonicalJSON(schema.const) !== canonicalJSON(value)) {
    fail(pointer, `Must equal ${preview(schema.const)}`);
    return;
  }

  if (typeof value === 'number') {
    const { minimum, maximum, exclusiveMinimum, exclusiveMaximum } = schema;
    if (typeof minimum === 'number') {
      if (exclusiveMinimum === true ? value <= minimum : value < minimum) {
        fail(pointer, exclusiveMinimum === true ? `Must be greater than ${minimum}` : `Must be at least ${minimum}`);
      }
    }
    if (typeof maximum === 'number') {
      if (exclusiveMaximum === true ? value >= maximum : value > maximum) {
        fail(pointer, exclusiveMaximum === true ? `Must be less than ${maximum}` : `Must be at most ${maximum}`);
      }
    }
    if (typeof exclusiveMinimum === 'number' && value <= exclusiveMinimum) fail(pointer, `Must be greater than ${exclusiveMinimum}`);
    if (typeof exclusiveMaximum === 'number' && value >= exclusiveMaximum) fail(pointer, `Must be less than ${exclusiveMaximum}`);
  }

  if (typeof value === 'string') {
    const length = [...value].length;
    if (typeof schema.minLength === 'number' && length < schema.minLength) {
      fail(pointer, `Must be at least ${schema.minLength} ${schema.minLength === 1 ? 'character' : 'characters'}`);
    }
    if (typeof schema.maxLength === 'number' && length > schema.maxLength) {
      fail(pointer, `Must be at most ${schema.maxLength} ${schema.maxLength === 1 ? 'character' : 'characters'}`);
    }
    if (typeof schema.pattern === 'string') {
      const re = compilePattern(schema.pattern);
      if (re && !re.test(value)) fail(pointer, `Must match the pattern ${schema.pattern}`);
    }
  }

  if (Array.isArray(value)) {
    if (typeof schema.minItems === 'number' && value.length < schema.minItems) {
      fail(pointer, `Must have at least ${schema.minItems} ${schema.minItems === 1 ? 'item' : 'items'}`);
    }
    if (typeof schema.maxItems === 'number' && value.length > schema.maxItems) {
      fail(pointer, `Must have at most ${schema.maxItems} ${schema.maxItems === 1 ? 'item' : 'items'}`);
    }
    if (isPlainObject(schema.items)) {
      value.forEach((item, i) => validateNode(schema.items, item, childPointer(pointer, i), errors));
    }
  }

  if (isPlainObject(value)) {
    for (const name of requiredList(schema)) {
      if (!(name in value)) fail(childPointer(pointer, name), 'Required');
    }
    const props = isPlainObject(schema.properties) ? schema.properties : {};
    for (const [key, child] of Object.entries(value)) {
      if (Object.prototype.hasOwnProperty.call(props, key)) {
        validateNode(props[key], child, childPointer(pointer, key), errors);
      } else if (schema.additionalProperties === false) {
        fail(childPointer(pointer, key), 'Not allowed by the schema');
      }
    }
  }
}

/**
 * Checks an argument object against the supported schema subset: type
 * (including integer), required, enum, const, numeric bounds, string length,
 * pattern, item counts, nested properties and required, single-schema items,
 * and additionalProperties false. Other keywords never produce an error; they
 * set uncheckedKeywords so the UI can say the server validates the call.
 */
export function validateArguments(schema: unknown, args: unknown): ValidationResult {
  const errors: Record<string, string> = {};
  validateNode(schema, args, '', errors);
  return { errors, uncheckedKeywords: hasUncheckedKeywords(schema) };
}
