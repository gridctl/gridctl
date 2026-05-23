// Cryptographically secure random-string generation for the secret generator.
// Uses the Web Crypto CSPRNG (crypto.getRandomValues) with rejection sampling
// to avoid modulo bias — never Math.random. See CONSTITUTION Article XII.

export interface SecretOptions {
  length: number;
  upper: boolean;
  lower: boolean;
  digits: boolean;
  symbols: boolean;
}

const UPPER = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ';
const LOWER = 'abcdefghijklmnopqrstuvwxyz';
const DIGITS = '0123456789';
const SYMBOLS = '!@#$%^&*-_=+';

// buildAlphabet assembles the character pool from the enabled classes.
export function buildAlphabet(opts: SecretOptions): string {
  return (
    (opts.upper ? UPPER : '') +
    (opts.lower ? LOWER : '') +
    (opts.digits ? DIGITS : '') +
    (opts.symbols ? SYMBOLS : '')
  );
}

// entropyBits returns the Shannon entropy (length × log2(alphabetSize)) of a
// string drawn uniformly from the alphabet. Zero for a degenerate alphabet.
export function entropyBits(length: number, alphabetSize: number): number {
  if (length <= 0 || alphabetSize <= 1) return 0;
  return length * Math.log2(alphabetSize);
}

// generateSecret returns a random string of opts.length characters drawn
// uniformly from the enabled alphabet. Bytes come from the CSPRNG and are
// mapped with rejection sampling: any byte at or above the largest multiple of
// the alphabet size that fits in a byte is discarded, so every character is
// equally likely (no modulo bias).
export function generateSecret(opts: SecretOptions): string {
  const alphabet = buildAlphabet(opts);
  if (alphabet.length === 0) {
    throw new Error('at least one character class must be enabled');
  }
  if (opts.length <= 0) return '';

  const setLen = alphabet.length;
  const max = 256 - (256 % setLen); // largest multiple of setLen within a byte
  const out: string[] = [];
  // Over-allocate so rejections rarely force a second getRandomValues call.
  const buf = new Uint8Array(opts.length * 2);
  while (out.length < opts.length) {
    crypto.getRandomValues(buf);
    for (let i = 0; i < buf.length && out.length < opts.length; i++) {
      const b = buf[i];
      if (b < max) out.push(alphabet[b % setLen]);
    }
  }
  return out.join('');
}
