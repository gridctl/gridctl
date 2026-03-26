import { create } from 'zustand';

export interface PlaygroundMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  timestamp: string;
  isStreaming?: boolean;
  error?: boolean;
}

export interface ProviderAuth {
  apiKey: boolean;
  keyName: string | null;
  cliPath: string | null;
}

export interface PlaygroundAuthStatus {
  providers: Record<string, ProviderAuth>;
  ollama: { reachable: boolean; endpoint: string };
}

interface PlaygroundState {
  messages: PlaygroundMessage[];
  isLoading: boolean;
  error: string | null;
  authStatus: PlaygroundAuthStatus | null;
  authLoading: boolean;
  sessionId: string;

  addMessage: (msg: PlaygroundMessage) => void;
  updateStreamingMessage: (id: string, content: string) => void;
  finalizeMessage: (id: string, content: string, error?: boolean) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setAuthStatus: (status: PlaygroundAuthStatus | null) => void;
  setAuthLoading: (loading: boolean) => void;
  clearMessages: () => void;
}

function makeSessionId(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return Math.random().toString(36).slice(2) + Date.now().toString(36);
}

export const usePlaygroundStore = create<PlaygroundState>((set) => ({
  messages: [],
  isLoading: false,
  error: null,
  authStatus: null,
  authLoading: false,
  sessionId: makeSessionId(),

  addMessage: (msg) =>
    set((s) => ({ messages: [...s.messages, msg] })),

  updateStreamingMessage: (id, content) =>
    set((s) => ({
      messages: s.messages.map((m) => (m.id === id ? { ...m, content } : m)),
    })),

  finalizeMessage: (id, content, error) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.id === id ? { ...m, content, isStreaming: false, ...(error ? { error: true } : {}) } : m
      ),
    })),

  setLoading: (isLoading) => set({ isLoading }),
  setError: (error) => set({ error }),
  setAuthStatus: (authStatus) => set({ authStatus }),
  setAuthLoading: (authLoading) => set({ authLoading }),
  clearMessages: () => set({ messages: [], error: null }),
}));
