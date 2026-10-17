import { useCallback, useRef, useState } from 'react';
import { detectServerCard, ProbeError, type DetectedCard, type DetectedCardServer } from '../lib/api';
import { getWizardSessionId } from '../lib/wizardSession';

export interface DetectCardState {
  loading: boolean;
  error: ProbeError | Error | null;
  result: DetectedCard | null;
}

export interface UseDetectCard extends DetectCardState {
  detect: (url: string) => Promise<DetectedCard | null>;
  reset: () => void;
}

const initialState: DetectCardState = {
  loading: false,
  error: null,
  result: null,
};

/**
 * useDetectCard wraps POST /api/servers/card. A later click cancels the
 * previous request's result so a slow response cannot overwrite a newer one.
 */
export function useDetectCard(): UseDetectCard {
  const [state, setState] = useState<DetectCardState>(initialState);
  const generation = useRef(0);

  const reset = useCallback(() => {
    generation.current += 1;
    setState(initialState);
  }, []);

  const detect = useCallback(async (url: string): Promise<DetectedCard | null> => {
    const current = generation.current + 1;
    generation.current = current;
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      const result = await detectServerCard(url, getWizardSessionId());
      if (generation.current !== current) return null;
      setState({ loading: false, error: null, result });
      return result;
    } catch (err) {
      if (generation.current !== current) return null;
      setState({
        loading: false,
        error: err instanceof Error ? err : new Error(String(err)),
        result: null,
      });
      return null;
    }
  }, []);

  return { ...state, detect, reset };
}

export interface CardPrefill {
  url: string;
  transport: string;
  auth?: {
    type: 'bearer' | 'header';
    token?: string;
    header?: string;
    value?: string;
  };
}

/** Form patch from a card draft. Literal credential values are dropped. */
export function cardPrefill(server: DetectedCardServer): CardPrefill {
  const base: CardPrefill = { url: server.url, transport: server.transport };
  const auth = server.auth;
  if (!auth) return base;
  if (auth.type === 'bearer') {
    return { ...base, auth: { type: 'bearer', token: placeholderOnly(auth.token) } };
  }
  if (auth.type === 'header') {
    return {
      ...base,
      auth: { type: 'header', header: auth.header, value: placeholderOnly(auth.value) },
    };
  }
  return base;
}

function placeholderOnly(value: string | undefined): string | undefined {
  if (!value || !value.startsWith('${var:')) return undefined;
  return value;
}
