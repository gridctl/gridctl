import * as YAML from 'yaml';
import { maskYAMLSecretsWith, registerYamlModule } from './wizard-secret-fields';

// wizard-secrets.ts: credential-field helpers for the wizard. The yaml-free
// parts live in wizard-secret-fields.ts so the wizard store can use them
// without pulling the yaml package into the Stack chunk.
export { isLiteralSecret, isSensitiveEnvName, redactFormData, type RedactableType } from './wizard-secret-fields';

registerYamlModule(YAML);

// maskYAMLSecrets replaces the source text of each literal secret scalar with
// replacement (raw YAML text) and leaves every other character as authored.
// Returns null when the YAML does not parse.
export function maskYAMLSecrets(yaml: string, replacement: string): { yaml: string; masked: number } | null {
  return maskYAMLSecretsWith(YAML, yaml, replacement);
}
