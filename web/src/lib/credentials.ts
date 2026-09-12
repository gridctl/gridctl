export interface GatewayCredential {
  mode: 'bearer' | 'api_key';
  header: string;
  token: string;
}

export const credentialKey = 'gridctl-auth-credential';
export const legacyTokenKey = 'gridctl-auth-token';
export const windowOnlyNotice = 'Verified for this window only. Refresh and new detached windows require re-entry. Other windows may still use the previously saved credential.';

export function readCredential(): { credential: GatewayCredential | null; notice: string | null } {
  try {
    const stored = localStorage.getItem(credentialKey);
    if (stored !== null) {
      const data = JSON.parse(stored);
      if (data?.version !== 1) return { credential: null, notice: 'Unsupported saved credential version. Re-enter your credential.' };
      if ((data.mode !== 'bearer' && data.mode !== 'api_key') || typeof data.token !== 'string' || typeof data.header !== 'string') {
        return { credential: null, notice: 'Saved credential metadata is invalid. Re-enter your credential.' };
      }
      return usableStoredCredential({ mode: data.mode, header: data.header, token: data.token });
    }
    const token = localStorage.getItem(legacyTokenKey);
    return token === null ? { credential: null, notice: null } : usableStoredCredential({ mode: 'bearer', header: 'Authorization', token });
  } catch {
    return { credential: null, notice: 'Saved credentials could not be read. You can verify a credential for this window.' };
  }
}

function usableStoredCredential(credential: GatewayCredential): { credential: GatewayCredential | null; notice: string | null } {
  try {
    credentialHeaders(credential);
    return { credential, notice: null };
  } catch {
    return { credential: null, notice: 'The saved credential cannot be sent by browser Fetch. Re-enter a browser-compatible credential; native client settings are unchanged.' };
  }
}

export function persistCredential(credential: GatewayCredential): string | null {
  try {
    const existing = localStorage.getItem(credentialKey);
    const previous = existing === null ? {} : JSON.parse(existing);
    if (existing !== null && previous?.version !== 1) return windowOnlyNotice;
    // One versioned entry avoids pairing a new mode with an older token when
    // storage fails between writes. Legacy token text is never parsed as JSON.
    const entry = JSON.stringify({ ...previous, version: 1, ...credential });
    localStorage.setItem(credentialKey, entry);
    if (localStorage.getItem(credentialKey) !== entry) return windowOnlyNotice;
    return null;
  } catch {
    return windowOnlyNotice;
  }
}

const forbidden = new Set([
  'accept-charset', 'accept-encoding', 'access-control-request-headers', 'access-control-request-method',
  'connection', 'content-length', 'cookie', 'cookie2', 'date', 'dnt', 'expect', 'host', 'keep-alive',
  'origin', 'permissions-policy', 'referer', 'set-cookie', 'te', 'trailer', 'transfer-encoding',
  'upgrade', 'via', 'user-agent', 'x-http-method', 'x-http-method-override', 'x-method-override',
]);
const protocolOwned = new Set(['accept', 'content-type', 'mcp-session-id', 'mcp-protocol-version', 'last-event-id']);

export function credentialHeaders(credential: GatewayCredential | null, extra?: HeadersInit): Headers {
  const headers = new Headers(extra);
  if (!credential) return headers;
  const name = credential.mode === 'bearer' ? 'Authorization' : credential.header || 'Authorization';
  const lower = name.toLowerCase();
  if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name)) throw new Error('Credential header name must be a valid HTTP field name.');
  if (forbidden.has(lower) || lower.startsWith('proxy-') || lower.startsWith('sec-')) {
    throw new Error('This credential header is not supported by browser Fetch. Use a supported header or a native client.');
  }
  if (protocolOwned.has(lower) || headers.has(name)) throw new Error('Credential header conflicts with a request or protocol header.');
  const codes = Array.from(credential.token, c => c.charCodeAt(0));
  if (codes.some(c => c < 32 || c === 127)) throw new Error('Credential values cannot contain control characters.');
  const value = credential.mode === 'bearer' ? `Bearer ${credential.token}` : credential.token;
  // Fetch normalizes edge whitespace and cannot represent non-ByteString
  // values. Refuse those browser limitations instead of changing opaque bytes.
  if (value.startsWith(' ') || value.endsWith(' ') || codes.some(c => c > 255)) {
    throw new Error('This opaque credential value cannot be represented unchanged by browser Fetch. Use a native client.');
  }
  headers.set(name, value);
  return headers;
}
