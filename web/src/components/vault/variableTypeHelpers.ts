import type { VariableType } from '../../lib/api';

// validateVariableInput mirrors the Go side's validateAndNormalize: callers
// get a normalized value to send to the API and a human-readable error when
// the input doesn't match its declared type. PR 1 wires the validation but
// expansion still treats values as opaque strings.
export function validateVariableInput(
  type: VariableType,
  value: string,
): { ok: true; normalized: string } | { ok: false; error: string } {
  if (value === '' && type !== 'string') {
    return { ok: false, error: `value required for type=${type}` };
  }
  switch (type) {
    case 'string':
      return { ok: true, normalized: value };
    case 'json':
      try {
        JSON.parse(value);
        return { ok: true, normalized: value };
      } catch (e) {
        return {
          ok: false,
          error: `invalid JSON: ${e instanceof Error ? e.message : String(e)}`,
        };
      }
    case 'list': {
      const parts = value
        .split(',')
        .map((p) => p.trim())
        .filter((p) => p !== '');
      return { ok: true, normalized: JSON.stringify(parts) };
    }
    case 'number':
      if (Number.isFinite(Number(value)) && value.trim() !== '') {
        return { ok: true, normalized: value };
      }
      return { ok: false, error: `invalid number: "${value}"` };
    case 'bool': {
      const v = value.trim().toLowerCase();
      if (['true', 'false', '1', '0', 'yes', 'no', 't', 'f'].includes(v)) {
        return { ok: true, normalized: value };
      }
      return { ok: false, error: `invalid bool: "${value}"` };
    }
  }
}
