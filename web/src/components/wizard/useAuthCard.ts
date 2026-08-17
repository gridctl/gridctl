import { useCallback, useState } from 'react';
import type { SkillAuth } from '../../lib/api';

export type AuthMode = 'vault' | 'token';

/**
 * Controller for the credentials card. State lives with the consumer so each
 * wizard owns its own instance and can build a payload at submit time; the
 * card itself stays presentational.
 */
export interface AuthCardController {
  open: boolean;
  setOpen: (open: boolean) => void;
  mode: AuthMode;
  setMode: (mode: AuthMode) => void;
  vaultRef: string;
  setVaultRef: (ref: string) => void;
  pasteToken: string;
  setPasteToken: (token: string) => void;
  banner: string | null;
  /** Open the card with a banner, announcing why it opened. */
  openWithBanner: (message: string) => void;
  /** Open in vault mode with a banner — the ssh-to-HTTPS switch path. */
  openInVaultMode: (message: string) => void;
  clearBanner: () => void;
  /**
   * The payload for a request, or undefined for "ambient", which omits the
   * field entirely and preserves public-repo behavior. On the pack endpoints
   * omitting it also lets the server fall back to a stored reference.
   */
  buildAuth: (ssh: boolean) => SkillAuth | undefined;
}

export function useAuthCard(): AuthCardController {
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<AuthMode>('vault');
  const [vaultRef, setVaultRef] = useState('');
  const [pasteToken, setPasteToken] = useState('');
  const [banner, setBanner] = useState<string | null>(null);

  const openWithBanner = useCallback((message: string) => {
    setOpen(true);
    setBanner(message);
  }, []);

  const openInVaultMode = useCallback((message: string) => {
    setMode('vault');
    setOpen(true);
    setBanner(message);
  }, []);

  const clearBanner = useCallback(() => setBanner(null), []);

  const buildAuth = useCallback(
    (ssh: boolean): SkillAuth | undefined => {
      if (ssh) return undefined; // ambient ssh-agent
      if (mode === 'vault' && vaultRef) return { method: 'token', credentialRef: vaultRef };
      // Tokens pasted from a provider UI carry trailing whitespace, which
      // fails auth in a way that looks like a wrong token.
      const trimmed = pasteToken.trim();
      if (mode === 'token' && trimmed) return { method: 'token', token: trimmed };
      return undefined;
    },
    [mode, vaultRef, pasteToken],
  );

  return {
    open,
    setOpen,
    mode,
    setMode,
    vaultRef,
    setVaultRef,
    pasteToken,
    setPasteToken,
    banner,
    openWithBanner,
    openInVaultMode,
    clearBanner,
    buildAuth,
  };
}
