import type { YamlModule } from './lineIssues';

// wizard-secret-fields.ts: the wizard's definition of a credential field and a
// literal secret, mirroring checkExportCredentials in pkg/config/export.go so
// the browser and `gridctl export` agree on what must not be kept.

// Mirrors expandRegex in pkg/config/expand.go.
const REFERENCE_PATTERN =
  /\$\{(?:(?:vault|var):)?[a-zA-Z_][a-zA-Z0-9_]*(?::([+-])([^}]*))?\}|\$[a-zA-Z_][a-zA-Z0-9_]*|\$\{secret:[a-z][a-z0-9-]*\/[^}]+\}/g;

const SENSITIVE_ENV_PARTS = ['PASSWORD', 'SECRET', 'TOKEN', 'API_KEY', 'APIKEY', 'PRIVATE_KEY', 'ACCESS_KEY', 'AUTH', 'CREDENTIAL'];

// isLiteralSecret reports whether a credential value would land in the stack
// file as an inline secret: it holds no reference, or a reference carries a
// non-empty default or replacement operand (export rejects both).
export function isLiteralSecret(value: string | undefined): boolean {
  if (!value) return false;
  const matches = [...value.matchAll(REFERENCE_PATTERN)];
  if (matches.length === 0) return true;
  return matches.some((m) => m[1] !== undefined && m[2] !== undefined && m[2] !== '');
}

export function isSensitiveEnvName(key: string): boolean {
  const upper = key.toUpperCase();
  return SENSITIVE_ENV_PARTS.some((part) => upper.includes(part));
}

export type RedactableType = 'stack' | 'mcp-server' | 'resource';

type Obj = Record<string, unknown>;

function asObj(value: unknown): Obj | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Obj) : null;
}

function blankField(obj: Obj | null, key: string): number {
  if (!obj) return 0;
  const value = obj[key];
  if (typeof value === 'string' && isLiteralSecret(value)) {
    obj[key] = '';
    return 1;
  }
  return 0;
}

function blankMap(map: unknown, sensitive: (key: string) => boolean): number {
  const obj = asObj(map);
  if (!obj) return 0;
  let count = 0;
  for (const key of Object.keys(obj)) {
    if (sensitive(key)) count += blankField(obj, key);
  }
  return count;
}

function redactServer(server: unknown): number {
  const obj = asObj(server);
  if (!obj) return 0;
  const auth = asObj(obj.auth);
  return (
    blankField(auth, 'token') +
    blankField(auth, 'value') +
    blankField(auth, 'clientSecret') +
    blankMap(obj.headers, () => true) +
    blankMap(obj.env, isSensitiveEnvName) +
    blankMap(obj.buildArgs, isSensitiveEnvName)
  );
}

function redactResource(resource: unknown): number {
  return blankMap(asObj(resource)?.env, isSensitiveEnvName);
}

function redactStack(stack: unknown): number {
  const obj = asObj(stack);
  if (!obj) return 0;
  let count = blankField(asObj(asObj(obj.gateway)?.auth), 'token');
  if (Array.isArray(obj.mcpServers)) for (const s of obj.mcpServers) count += redactServer(s);
  if (Array.isArray(obj.resources)) for (const r of obj.resources) count += redactResource(r);
  return count;
}

// redactFormData returns a deep copy of one wizard form with every literal
// in a credential field replaced by '' and the number of values removed.
// References are kept. The input is not modified.
export function redactFormData<T>(type: RedactableType, data: T): { data: T; redacted: number } {
  if (data === null || data === undefined || typeof data !== 'object') return { data, redacted: 0 };
  const copy = structuredClone(data);
  const redacted =
    type === 'stack' ? redactStack(copy) : type === 'mcp-server' ? redactServer(copy) : redactResource(copy);
  return { data: copy, redacted };
}

// The yaml package stays out of the Stack chunk, which reaches the wizard
// store. wizard-secrets.ts registers the module when the wizard loads, and
// only the wizard can edit YAML, so the store masks through this hook.
let registeredYaml: YamlModule | null = null;

export function registerYamlModule(mod: YamlModule): void {
  registeredYaml = mod;
}

// maskLoadedYAMLSecrets is maskYAMLSecrets through the registered yaml module.
// Returns undefined when no module has been registered yet.
export function maskLoadedYAMLSecrets(yaml: string, replacement: string): { yaml: string; masked: number } | null | undefined {
  return registeredYaml ? maskYAMLSecretsWith(registeredYaml, yaml, replacement) : undefined;
}

function keyOf(Y: YamlModule, pair: unknown): string | null {
  if (!Y.isPair(pair)) return null;
  const key = pair.key;
  if (Y.isScalar(key)) return String(key.value);
  return null;
}

// parentKey names the mapping that owns a pair whose map sits under path[i].
// A merge key (`<<: {...}` or `<<: [{...}, ...]`) folds its maps into the
// enclosing mapping when loaded, so it is skipped in favor of that mapping.
function parentKey(Y: YamlModule, path: readonly unknown[], i: number): string | null {
  for (;;) {
    if (Y.isSeq(path[i]) && keyOf(Y, path[i - 1]) === '<<') i -= 1;
    const name = keyOf(Y, path[i]);
    if (name !== '<<') return name;
    i -= 2;
  }
}

// maskYAMLSecrets replaces the source text of each literal secret scalar with
// replacement (raw YAML text) and leaves every other character as authored.
// The document is parsed only to locate scalars; it is never re-serialized,
// because stringify refolds long lines and rewrites flow collections.
// Returns null when the YAML does not parse.
export function maskYAMLSecretsWith(Y: YamlModule, yaml: string, replacement: string): { yaml: string; masked: number } | null {
  const doc = Y.parseDocument(yaml);
  if (doc.errors.length > 0) return null;

  const targets: Array<[number, number]> = [];
  Y.visit(doc, {
    Scalar(key, node, path) {
      if (key !== 'value' || !node.range) return;
      const pair = path[path.length - 1];
      const name = keyOf(Y, pair);
      if (name === null) return;
      const map = path[path.length - 2];
      if (!Y.isMap(map)) return;
      const parent = parentKey(Y, path, path.length - 3);
      const sensitive =
        (parent === 'auth' && (name === 'token' || name === 'value' || name === 'client_secret')) ||
        parent === 'headers' ||
        ((parent === 'env' || parent === 'build_args') && isSensitiveEnvName(name));
      if (!sensitive) return;
      // A number or boolean still loads into a string field as a literal.
      const value = node.value;
      if (value === null || value === undefined || typeof value === 'object') return;
      if (!isLiteralSecret(String(value))) return;
      targets.push([node.range[0], node.range[1]]);
    },
  });

  if (targets.length === 0) return { yaml, masked: 0 };
  targets.sort((a, b) => a[0] - b[0]);
  let out = yaml;
  for (let i = targets.length - 1; i >= 0; i--) {
    const [start, end] = targets[i];
    const slice = out.slice(start, end);
    const text = slice.endsWith('\n') ? `${replacement}\n` : replacement;
    out = out.slice(0, start) + text + out.slice(end);
  }
  return { yaml: out, masked: targets.length };
}
