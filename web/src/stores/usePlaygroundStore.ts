import { create } from 'zustand';

export interface TurnMetrics {
  tokensIn: number;
  tokensOut: number;
  formatSavingsPct: number;
}

export interface WaterfallEntry {
  id: string;
  toolName: string;
  serverName: string;
  input?: unknown;
  output?: unknown;
  durationMs?: number;
  status: 'pending' | 'complete';
  startedAt: string;
}

export interface PlaygroundMessage {
  id: string;
  role: 'user' | 'assistant' | 'session-start';
  content: string;
  timestamp: string;
  isStreaming?: boolean;
  error?: boolean;
  metrics?: TurnMetrics;
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

  // Model selector state
  selectedModel: string | null; // null = use auto-detected default from auth
  ollamaEndpoint: string;       // configurable Ollama base URL

  // SSE / inference state
  agentIsThinking: boolean;
  activeToolCallEdges: Set<string>; // server names of active tool calls (mapped to edges in Phase 6)
  waterfallEntries: WaterfallEntry[];

  addMessage: (msg: PlaygroundMessage) => void;
  updateStreamingMessage: (id: string, content: string) => void;
  finalizeMessage: (id: string, content: string, metrics?: TurnMetrics | null, error?: boolean) => void;
  setLoading: (loading: boolean) => void;
  setError: (error: string | null) => void;
  setAuthStatus: (status: PlaygroundAuthStatus | null) => void;
  setAuthLoading: (loading: boolean) => void;
  clearMessages: () => void;
  setSelectedModel: (model: string | null) => void;
  setOllamaEndpoint: (endpoint: string) => void;

  setAgentIsThinking: (thinking: boolean) => void;
  addActiveToolCallEdge: (serverName: string) => void;
  removeActiveToolCallEdge: (serverName: string) => void;
  addWaterfallEntry: (entry: WaterfallEntry) => void;
  updateWaterfallEntry: (id: string, update: Partial<WaterfallEntry>) => void;
  clearWaterfall: () => void;
  resetSession: () => void;
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

  selectedModel: null,
  ollamaEndpoint: 'http://localhost:11434/v1',

  agentIsThinking: false,
  activeToolCallEdges: new Set<string>(),
  waterfallEntries: [],

  addMessage: (msg) =>
    set((s) => ({ messages: [...s.messages, msg] })),

  updateStreamingMessage: (id, content) =>
    set((s) => ({
      messages: s.messages.map((m) => (m.id === id ? { ...m, content } : m)),
    })),

  finalizeMessage: (id, content, metrics, error) =>
    set((s) => ({
      messages: s.messages.map((m) =>
        m.id === id
          ? {
              ...m,
              content,
              isStreaming: false,
              ...(metrics ? { metrics } : {}),
              ...(error ? { error: true } : {}),
            }
          : m
      ),
    })),

  setLoading: (isLoading) => set({ isLoading }),
  setError: (error) => set({ error }),
  setAuthStatus: (authStatus) => set({ authStatus }),
  setAuthLoading: (authLoading) => set({ authLoading }),
  clearMessages: () => set({ messages: [], error: null, waterfallEntries: [], agentIsThinking: false, activeToolCallEdges: new Set() }),
  setSelectedModel: (selectedModel) => set({ selectedModel }),
  setOllamaEndpoint: (ollamaEndpoint) => set({ ollamaEndpoint }),

  setAgentIsThinking: (agentIsThinking) => set({ agentIsThinking }),

  addActiveToolCallEdge: (serverName) =>
    set((s) => ({ activeToolCallEdges: new Set([...s.activeToolCallEdges, serverName]) })),

  removeActiveToolCallEdge: (serverName) =>
    set((s) => {
      const next = new Set(s.activeToolCallEdges);
      next.delete(serverName);
      return { activeToolCallEdges: next };
    }),

  addWaterfallEntry: (entry) =>
    set((s) => ({ waterfallEntries: [...s.waterfallEntries, entry] })),

  updateWaterfallEntry: (id, update) =>
    set((s) => ({
      waterfallEntries: s.waterfallEntries.map((e) => (e.id === id ? { ...e, ...update } : e)),
    })),

  clearWaterfall: () => set({ waterfallEntries: [] }),

  resetSession: () =>
    set({
      messages: [],
      error: null,
      waterfallEntries: [],
      agentIsThinking: false,
      activeToolCallEdges: new Set(),
      isLoading: false,
      sessionId: makeSessionId(),
    }),
}));
