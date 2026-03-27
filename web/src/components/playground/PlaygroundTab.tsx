import { useEffect, useRef, useState, useCallback } from 'react';
import {
  FlaskConical,
  Send,
  Trash2,
  RefreshCw,
  AlertCircle,
  CheckCircle2,
  WifiOff,
  Zap,
  Bot,
  User,
  ChevronDown,
  Cpu,
} from 'lucide-react';
import { marked } from 'marked';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { useUIStore } from '../../stores/useUIStore';
import { usePlaygroundStore, type TurnMetrics, type WaterfallEntry } from '../../stores/usePlaygroundStore';
import { ReasoningWaterfall } from './ReasoningWaterfall';
import {
  fetchPlaygroundAuth,
  sendPlaygroundChat,
  buildStreamHeaders,
  type PlaygroundAuthResponse,
} from '../../lib/api';

// ─── Model selector constants ─────────────────────────────────────────────────

interface ModelOption {
  value: string;
  label: string;
  provider: string;
  shortLabel: string;
}

const MODEL_OPTIONS: ModelOption[] = [
  { value: 'claude-3-5-sonnet-latest', label: 'Claude 3.5 Sonnet', shortLabel: '3.5 Sonnet', provider: 'anthropic' },
  { value: 'claude-3-7-sonnet-latest', label: 'Claude 3.7 Sonnet', shortLabel: '3.7 Sonnet', provider: 'anthropic' },
  { value: 'gpt-4o', label: 'GPT-4o', shortLabel: 'GPT-4o', provider: 'openai' },
  { value: 'gpt-4o-mini', label: 'GPT-4o mini', shortLabel: '4o-mini', provider: 'openai' },
  { value: 'gemini-2.0-flash', label: 'Gemini 2.0 Flash', shortLabel: '2.0 Flash', provider: 'gemini' },
  { value: 'gemini-2.5-pro', label: 'Gemini 2.5 Pro', shortLabel: '2.5 Pro', provider: 'gemini' },
  { value: 'ollama', label: 'Ollama (local)', shortLabel: 'Ollama', provider: 'ollama' },
];

const PROVIDER_COLORS: Record<string, string> = {
  anthropic: 'text-amber-400',
  openai: 'text-green-400',
  gemini: 'text-blue-400',
  ollama: 'text-secondary',
};

function inferAuthModeFromModel(model: string): string {
  if (model === 'ollama') return 'LOCAL_LLM';
  return 'API_KEY';
}

// ─── Model selector component ─────────────────────────────────────────────────

function ModelSelector({
  selectedModel,
  ollamaEndpoint,
  onSelectModel,
  onOllamaEndpointChange,
  defaultLabel,
}: {
  selectedModel: string | null;
  ollamaEndpoint: string;
  onSelectModel: (model: string | null) => void;
  onOllamaEndpointChange: (endpoint: string) => void;
  defaultLabel: string;
}) {
  const [open, setOpen] = useState(false);

  const active = MODEL_OPTIONS.find((m) => m.value === selectedModel);
  const providerColor = active ? (PROVIDER_COLORS[active.provider] ?? 'text-text-secondary') : 'text-text-muted';

  const grouped: Record<string, ModelOption[]> = {};
  for (const m of MODEL_OPTIONS) {
    if (!grouped[m.provider]) grouped[m.provider] = [];
    grouped[m.provider].push(m);
  }

  return (
    <div className="relative">
      <button
        onClick={() => setOpen((v) => !v)}
        className={cn(
          'flex items-center gap-1.5 px-2 py-1 rounded-md text-[10px] transition-colors',
          'bg-surface-elevated/40 border border-border/30 hover:border-border/60',
          open && 'border-secondary/30 bg-surface-elevated/60',
        )}
      >
        <Cpu size={9} className={providerColor} />
        <span className={cn('font-medium', providerColor)}>
          {active ? active.shortLabel : defaultLabel}
        </span>
        <ChevronDown size={9} className="text-text-muted/60" />
      </button>

      {open && (
        <div className="absolute bottom-full left-0 mb-1 z-50 w-52 glass-panel rounded-lg border border-border/50 shadow-xl overflow-hidden">
          {/* Auto-detect option */}
          <button
            onClick={() => { onSelectModel(null); setOpen(false); }}
            className={cn(
              'w-full flex items-center gap-2 px-3 py-2 text-[10px] text-left transition-colors',
              'hover:bg-surface-highlight/40',
              selectedModel === null ? 'text-secondary' : 'text-text-muted',
            )}
          >
            <span className="font-medium">Auto-detect</span>
            <span className="ml-auto text-[9px] text-text-muted/50">from auth</span>
          </button>
          <div className="border-t border-border/30" />

          {Object.entries(grouped).map(([provider, models]) => (
            <div key={provider}>
              <div className="px-3 py-1 text-[9px] uppercase tracking-wider text-text-muted/50 font-medium">
                {provider}
              </div>
              {models.map((m) => (
                <button
                  key={m.value}
                  onClick={() => { onSelectModel(m.value); setOpen(false); }}
                  className={cn(
                    'w-full flex items-center gap-2 px-3 py-1.5 text-[10px] text-left transition-colors',
                    'hover:bg-surface-highlight/40',
                    selectedModel === m.value ? PROVIDER_COLORS[provider] + ' bg-surface-elevated/40' : 'text-text-secondary',
                  )}
                >
                  <span className="font-mono">{m.label}</span>
                  {selectedModel === m.value && <span className="ml-auto text-[9px]">✓</span>}
                </button>
              ))}
            </div>
          ))}

          {/* Ollama endpoint field */}
          {selectedModel === 'ollama' && (
            <>
              <div className="border-t border-border/30 px-3 py-2">
                <label className="block text-[9px] text-text-muted mb-1">Endpoint URL</label>
                <input
                  type="text"
                  value={ollamaEndpoint}
                  onChange={(e) => onOllamaEndpointChange(e.target.value)}
                  onClick={(e) => e.stopPropagation()}
                  placeholder="http://localhost:11434/v1"
                  className={cn(
                    'w-full bg-surface-elevated border border-border/40 rounded px-2 py-1',
                    'text-[10px] font-mono text-text-primary outline-none',
                    'focus:border-secondary/40 transition-colors',
                  )}
                />
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ─── Auth helpers ────────────────────────────────────────────────────────────

type AuthLevel = 'api-key' | 'cli' | 'ollama' | 'none';

interface ResolvedAuth {
  level: AuthLevel;
  label: string;
  detail: string;
  authMode: string;
  model: string;
  ollamaUrl?: string;
}

function resolveAuth(status: PlaygroundAuthResponse | null): ResolvedAuth {
  if (!status) {
    return { level: 'none', label: 'Checking…', detail: '', authMode: 'API_KEY', model: 'claude-3-5-sonnet-latest' };
  }

  const { providers, ollama } = status;

  if (providers.anthropic?.apiKey) {
    return {
      level: 'api-key',
      label: 'Anthropic API key',
      detail: providers.anthropic.keyName ?? 'ANTHROPIC_API_KEY',
      authMode: 'API_KEY',
      model: 'claude-3-5-sonnet-latest',
    };
  }
  if (providers.openai?.apiKey) {
    return {
      level: 'api-key',
      label: 'OpenAI API key',
      detail: providers.openai.keyName ?? 'OPENAI_API_KEY',
      authMode: 'API_KEY',
      model: 'gpt-4o-mini',
    };
  }
  if (providers.gemini?.apiKey) {
    return {
      level: 'api-key',
      label: 'Gemini API key',
      detail: providers.gemini.keyName ?? 'GEMINI_API_KEY',
      authMode: 'API_KEY',
      model: 'gemini-2.0-flash',
    };
  }
  if (providers.anthropic?.cliPath) {
    return {
      level: 'cli',
      label: 'Claude CLI',
      detail: providers.anthropic.cliPath,
      authMode: 'CLI_PROXY',
      model: 'claude-3-5-sonnet-latest',
    };
  }
  if (providers.gemini?.cliPath) {
    return {
      level: 'cli',
      label: 'Gemini CLI',
      detail: providers.gemini.cliPath,
      authMode: 'CLI_PROXY',
      model: 'gemini-2.0-flash',
    };
  }
  if (ollama.reachable) {
    return {
      level: 'ollama',
      label: 'Ollama',
      detail: ollama.endpoint,
      authMode: 'LOCAL_LLM',
      model: 'llama3',
      ollamaUrl: ollama.endpoint,
    };
  }

  return {
    level: 'none',
    label: 'No auth configured',
    detail: 'Add an API key to Vault to get started',
    authMode: 'API_KEY',
    model: 'claude-3-5-sonnet-latest',
  };
}

// ─── Auth banner ─────────────────────────────────────────────────────────────

function AuthBanner({
  auth,
  loading,
  onRefresh,
}: {
  auth: ResolvedAuth;
  loading: boolean;
  onRefresh: () => void;
}) {
  const iconClass = {
    'api-key': 'text-status-running',
    cli: 'text-status-pending',
    ollama: 'text-secondary',
    none: 'text-status-error',
  }[auth.level];

  const dotClass = {
    'api-key': 'bg-status-running',
    cli: 'bg-status-pending',
    ollama: 'bg-secondary',
    none: 'bg-status-error',
  }[auth.level];

  const Icon = {
    'api-key': CheckCircle2,
    cli: Zap,
    ollama: FlaskConical,
    none: WifiOff,
  }[auth.level];

  return (
    <div
      className={cn(
        'flex items-center gap-2 px-3 h-9 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20',
        'text-[10px]'
      )}
    >
      {loading ? (
        <span className="flex items-center gap-1.5 text-text-muted">
          <span className="w-1.5 h-1.5 rounded-full bg-text-muted/50 animate-pulse" />
          Detecting auth…
        </span>
      ) : (
        <>
          <Icon size={11} className={iconClass} />
          <span className={cn('font-medium', iconClass)}>{auth.label}</span>
          {auth.detail && (
            <>
              <span className="text-border/80">·</span>
              <span className="text-text-muted font-mono truncate max-w-[280px]">{auth.detail}</span>
            </>
          )}
          {auth.level !== 'none' && (
            <span className="flex items-center gap-1 ml-1 text-[9px] text-status-running font-medium">
              <span className={cn('w-1.5 h-1.5 rounded-full', dotClass)} />
              ready
            </span>
          )}
        </>
      )}

      <div className="ml-auto">
        <IconButton
          icon={RefreshCw}
          onClick={onRefresh}
          tooltip="Refresh auth"
          size="sm"
          variant="ghost"
        />
      </div>
    </div>
  );
}

// ─── Session divider ──────────────────────────────────────────────────────────

function SessionDivider({ timestamp }: { timestamp: string }) {
  return (
    <div className="flex items-center gap-3 px-4 py-2">
      <div className="flex-1 h-px bg-border/20" />
      <span className="text-[9px] text-text-muted/50 tabular-nums tracking-wide uppercase">
        Session started · {new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
      </span>
      <div className="flex-1 h-px bg-border/20" />
    </div>
  );
}

// ─── Metrics badge ────────────────────────────────────────────────────────────

function MetricsBadge({ metrics }: { metrics: TurnMetrics }) {
  return (
    <div className="flex items-center gap-2 mt-1 text-[9px] text-text-muted/50 tabular-nums">
      <span>{metrics.tokensIn.toLocaleString()} in</span>
      <span className="text-border/40">·</span>
      <span>{metrics.tokensOut.toLocaleString()} out</span>
      {metrics.formatSavingsPct > 0 && (
        <>
          <span className="text-border/40">·</span>
          <span className="text-status-running/60">{metrics.formatSavingsPct.toFixed(1)}% savings</span>
        </>
      )}
    </div>
  );
}

// ─── Message bubble ───────────────────────────────────────────────────────────

function MessageBubble({ msg }: { msg: { id: string; role: string; content: string; timestamp: string; isStreaming?: boolean; error?: boolean; metrics?: TurnMetrics } }) {
  const isUser = msg.role === 'user';
  const isEmpty = !msg.content && msg.isStreaming;

  const html = !isUser && msg.content
    ? (marked.parse(msg.content, { breaks: true, gfm: true }) as string)
    : null;

  return (
    <div
      className={cn(
        'flex gap-2 px-4 py-2',
        isUser ? 'flex-row-reverse' : 'flex-row'
      )}
    >
      {/* Avatar */}
      <div
        className={cn(
          'w-5 h-5 rounded-md flex-shrink-0 flex items-center justify-center mt-0.5',
          isUser
            ? 'bg-primary/15 border border-primary/20'
            : 'bg-secondary/10 border border-secondary/20'
        )}
      >
        {isUser
          ? <User size={10} className="text-primary" />
          : <Bot size={10} className="text-secondary" />}
      </div>

      {/* Bubble */}
      <div className={cn('flex flex-col', isUser ? 'items-end' : 'items-start', 'max-w-[75%]')}>
        <div
          className={cn(
            'rounded-lg px-3 py-2 text-xs leading-relaxed w-full',
            isUser
              ? 'bg-primary/8 border border-primary/15 text-text-primary text-right'
              : msg.error
                ? 'bg-status-error/8 border border-status-error/20 text-status-error'
                : 'bg-surface-elevated border border-border/40 text-text-primary',
            isEmpty && 'min-w-[60px]'
          )}
        >
          {isEmpty ? (
            /* Thinking animation */
            <span className="flex items-center gap-1 h-3">
              {[0, 1, 2].map((i) => (
                <span
                  key={i}
                  className="w-1 h-1 rounded-full bg-secondary/60 animate-bounce"
                  style={{ animationDelay: `${i * 150}ms` }}
                />
              ))}
            </span>
          ) : msg.error && !msg.content ? (
            <span className="flex items-center gap-1.5">
              <AlertCircle size={11} />
              Request failed
            </span>
          ) : isUser ? (
            <span className="whitespace-pre-wrap">{msg.content}</span>
          ) : html ? (
            <div
              className="prose-playground"
              // marked output is safe for this use case (our own API responses)
              dangerouslySetInnerHTML={{ __html: html }}
            />
          ) : (
            <span>{msg.content}</span>
          )}

          {/* Timestamp */}
          <div
            className={cn(
              'text-[9px] mt-1 tabular-nums',
              isUser ? 'text-primary/40 text-right' : 'text-text-muted/50'
            )}
          >
            {new Date(msg.timestamp).toLocaleTimeString([], {
              hour: '2-digit',
              minute: '2-digit',
              second: '2-digit',
            })}
            {msg.isStreaming && !isEmpty && (
              <span className="ml-1.5 inline-block w-1.5 h-1.5 rounded-full bg-secondary animate-pulse align-middle" />
            )}
          </div>
        </div>

        {/* Metrics — shown below assistant message after streaming completes */}
        {!isUser && !msg.isStreaming && msg.metrics && (
          <MetricsBadge metrics={msg.metrics} />
        )}
      </div>
    </div>
  );
}

// ─── SSE event types ──────────────────────────────────────────────────────────

interface TokenEvent {
  type: 'token';
  data: { text: string };
}

interface ToolCallStartEvent {
  type: 'tool_call_start';
  data: { toolName: string; serverName: string; input: unknown };
}

interface ToolCallEndEvent {
  type: 'tool_call_end';
  data: { toolName: string; output: unknown; durationMs: number };
}

interface MetricsEvent {
  type: 'metrics';
  data: { tokens_in: number; tokens_out: number; format_savings_pct: number };
}

interface DoneEvent {
  type: 'done';
}

interface ErrorEvent {
  type: 'error';
  data: { message: string };
}

type SSEEvent = TokenEvent | ToolCallStartEvent | ToolCallEndEvent | MetricsEvent | DoneEvent | ErrorEvent;

// ─── Main component ───────────────────────────────────────────────────────────

export function PlaygroundTab() {
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const bottomPanelTab = useUIStore((s) => s.bottomPanelTab);
  const isVisible = bottomPanelOpen && bottomPanelTab === 'playground';

  const messages = usePlaygroundStore((s) => s.messages);
  const isLoading = usePlaygroundStore((s) => s.isLoading);
  const error = usePlaygroundStore((s) => s.error);
  const authStatus = usePlaygroundStore((s) => s.authStatus);
  const authLoading = usePlaygroundStore((s) => s.authLoading);
  const sessionId = usePlaygroundStore((s) => s.sessionId);
  const selectedModel = usePlaygroundStore((s) => s.selectedModel);
  const ollamaEndpoint = usePlaygroundStore((s) => s.ollamaEndpoint);
  const setSelectedModel = usePlaygroundStore((s) => s.setSelectedModel);
  const setOllamaEndpoint = usePlaygroundStore((s) => s.setOllamaEndpoint);
  const addMessage = usePlaygroundStore((s) => s.addMessage);
  const updateStreamingMessage = usePlaygroundStore((s) => s.updateStreamingMessage);
  const finalizeMessage = usePlaygroundStore((s) => s.finalizeMessage);
  const setLoading = usePlaygroundStore((s) => s.setLoading);
  const setError = usePlaygroundStore((s) => s.setError);
  const setAuthStatus = usePlaygroundStore((s) => s.setAuthStatus);
  const setAuthLoading = usePlaygroundStore((s) => s.setAuthLoading);
  const clearMessages = usePlaygroundStore((s) => s.clearMessages);
  const setAgentIsThinking = usePlaygroundStore((s) => s.setAgentIsThinking);
  const addActiveToolCallEdge = usePlaygroundStore((s) => s.addActiveToolCallEdge);
  const removeActiveToolCallEdge = usePlaygroundStore((s) => s.removeActiveToolCallEdge);
  const addWaterfallEntry = usePlaygroundStore((s) => s.addWaterfallEntry);
  const updateWaterfallEntry = usePlaygroundStore((s) => s.updateWaterfallEntry);
  const clearWaterfall = usePlaygroundStore((s) => s.clearWaterfall);

  const [input, setInput] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const streamingMsgIdRef = useRef<string | null>(null);
  const streamContentRef = useRef<string>('');
  const pendingMetricsRef = useRef<TurnMetrics | null>(null);
  const pendingToolCallsRef = useRef<Map<string, WaterfallEntry>>(new Map());
  const sseControllerRef = useRef<AbortController | null>(null);

  const resolvedAuth = resolveAuth(authStatus);

  // Override auth/model when user has explicitly selected a model
  const effectiveModel = selectedModel ?? resolvedAuth.model;
  const effectiveAuthMode = selectedModel ? inferAuthModeFromModel(selectedModel) : resolvedAuth.authMode;
  const effectiveOllamaUrl = selectedModel === 'ollama' ? ollamaEndpoint : resolvedAuth.ollamaUrl;

  const canSend = !isLoading && resolvedAuth.level !== 'none' && input.trim().length > 0;

  // ── Auth check ──────────────────────────────────────────────────────────

  const checkAuth = useCallback(async () => {
    setAuthLoading(true);
    try {
      const status = await fetchPlaygroundAuth();
      setAuthStatus(status);
    } catch {
      setAuthStatus(null);
    } finally {
      setAuthLoading(false);
    }
  }, [setAuthLoading, setAuthStatus]);

  useEffect(() => {
    if (!isVisible || authStatus !== null) return;
    checkAuth();
  }, [isVisible, authStatus, checkAuth]);

  // ── Auto-scroll ─────────────────────────────────────────────────────────

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  // ── Cleanup SSE on unmount ──────────────────────────────────────────────

  useEffect(() => {
    return () => {
      sseControllerRef.current?.abort();
    };
  }, []);

  // ── SSE reader ──────────────────────────────────────────────────────────

  const readSse = useCallback(
    async (signal: AbortSignal) => {
      const headers = buildStreamHeaders();
      let response: Response;

      try {
        response = await fetch(
          `/api/playground/stream?sessionId=${encodeURIComponent(sessionId)}`,
          { headers, signal }
        );
      } catch {
        return;
      }

      if (!response.ok || !response.body) return;

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      const cleanup = (isError: boolean) => {
        const msgId = streamingMsgIdRef.current;
        if (msgId) {
          finalizeMessage(msgId, streamContentRef.current || '', pendingMetricsRef.current, isError);
          streamingMsgIdRef.current = null;
          streamContentRef.current = '';
          pendingMetricsRef.current = null;
          pendingToolCallsRef.current.clear();
          setAgentIsThinking(false);
          setLoading(false);
        }
      };

      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split('\n');
          buffer = lines.pop() ?? '';

          for (const line of lines) {
            if (!line.startsWith('data: ')) continue;
            const raw = line.slice(6).trim();
            if (!raw) continue;

            let event: SSEEvent;
            try {
              event = JSON.parse(raw) as SSEEvent;
            } catch {
              continue;
            }

            const msgId = streamingMsgIdRef.current;

            switch (event.type) {
              case 'token': {
                if (!msgId) continue;
                streamContentRef.current += event.data.text;
                updateStreamingMessage(msgId, streamContentRef.current);
                break;
              }

              case 'tool_call_start': {
                const entryId = crypto.randomUUID();
                const entry: WaterfallEntry = {
                  id: entryId,
                  toolName: event.data.toolName,
                  serverName: event.data.serverName,
                  input: event.data.input,
                  status: 'pending',
                  startedAt: new Date().toISOString(),
                };
                pendingToolCallsRef.current.set(event.data.toolName, entry);
                addWaterfallEntry(entry);
                addActiveToolCallEdge(event.data.serverName);
                setAgentIsThinking(true);
                break;
              }

              case 'tool_call_end': {
                const pending = pendingToolCallsRef.current.get(event.data.toolName);
                if (pending) {
                  updateWaterfallEntry(pending.id, {
                    output: event.data.output,
                    durationMs: event.data.durationMs,
                    status: 'complete',
                  });
                  removeActiveToolCallEdge(pending.serverName);
                  pendingToolCallsRef.current.delete(event.data.toolName);
                }
                break;
              }

              case 'metrics': {
                pendingMetricsRef.current = {
                  tokensIn: event.data.tokens_in,
                  tokensOut: event.data.tokens_out,
                  formatSavingsPct: event.data.format_savings_pct,
                };
                break;
              }

              case 'done': {
                if (msgId) {
                  finalizeMessage(msgId, streamContentRef.current, pendingMetricsRef.current ?? undefined);
                  streamingMsgIdRef.current = null;
                  streamContentRef.current = '';
                  pendingMetricsRef.current = null;
                  pendingToolCallsRef.current.clear();
                  setAgentIsThinking(false);
                  setLoading(false);
                }
                sseControllerRef.current?.abort();
                return;
              }

              case 'error': {
                if (msgId) {
                  finalizeMessage(msgId, event.data.message || streamContentRef.current, null, true);
                  streamingMsgIdRef.current = null;
                  streamContentRef.current = '';
                  pendingMetricsRef.current = null;
                  pendingToolCallsRef.current.clear();
                  setAgentIsThinking(false);
                  setLoading(false);
                }
                sseControllerRef.current?.abort();
                return;
              }
            }
          }
        }
      } catch {
        // AbortError or read failure — clean up if we were still streaming
        cleanup(true);
      }
    },
    [
      sessionId,
      updateStreamingMessage,
      finalizeMessage,
      setLoading,
      setAgentIsThinking,
      addWaterfallEntry,
      updateWaterfallEntry,
      addActiveToolCallEdge,
      removeActiveToolCallEdge,
    ]
  );

  // ── Send message ────────────────────────────────────────────────────────

  const sendMessage = useCallback(async () => {
    const text = input.trim();
    if (!text || isLoading || resolvedAuth.level === 'none') return;

    setInput('');
    setError(null);

    // Add "Session started" divider at the beginning of each session
    const isFirstMessage = messages.length === 0;
    if (isFirstMessage) {
      addMessage({
        id: crypto.randomUUID(),
        role: 'session-start',
        content: '',
        timestamp: new Date().toISOString(),
      });
      clearWaterfall();
    }

    // Add user message
    addMessage({
      id: crypto.randomUUID(),
      role: 'user',
      content: text,
      timestamp: new Date().toISOString(),
    });

    // Add placeholder assistant message
    const assistantId = crypto.randomUUID();
    streamingMsgIdRef.current = assistantId;
    streamContentRef.current = '';
    pendingMetricsRef.current = null;
    addMessage({
      id: assistantId,
      role: 'assistant',
      content: '',
      timestamp: new Date().toISOString(),
      isStreaming: true,
    });

    setLoading(true);
    setAgentIsThinking(true);

    // Open SSE connection before posting
    const controller = new AbortController();
    sseControllerRef.current = controller;
    readSse(controller.signal);

    // Small delay so stream connection is established
    await new Promise((r) => setTimeout(r, 60));

    try {
      await sendPlaygroundChat({
        message: text,
        sessionId,
        authMode: effectiveAuthMode,
        model: effectiveModel,
        ...(effectiveOllamaUrl ? { ollamaUrl: effectiveOllamaUrl } : {}),
      });
    } catch (err: unknown) {
      controller.abort();
      setLoading(false);
      setAgentIsThinking(false);
      finalizeMessage(assistantId, '', null, true);
      streamingMsgIdRef.current = null;
      setError(err instanceof Error ? err.message : 'Failed to send message');
    }
  }, [
    input,
    isLoading,
    resolvedAuth,
    effectiveModel,
    effectiveAuthMode,
    effectiveOllamaUrl,
    sessionId,
    messages.length,
    addMessage,
    finalizeMessage,
    setLoading,
    setError,
    setAgentIsThinking,
    clearWaterfall,
    readSse,
  ]);

  // ── Keyboard handler ────────────────────────────────────────────────────

  function handleKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  }

  // ── Empty state ─────────────────────────────────────────────────────────

  const isEmpty = messages.length === 0;

  return (
    <div className="flex flex-col h-full">
      {/* Auth status bar */}
      <AuthBanner
        auth={resolvedAuth}
        loading={authLoading}
        onRefresh={checkAuth}
      />

      {/* Messages area */}
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark py-2">
        {isEmpty ? (
          <div className="flex flex-col items-center justify-center h-full gap-3 text-text-muted px-6 text-center">
            <div className="w-10 h-10 rounded-xl bg-secondary/8 border border-secondary/15 flex items-center justify-center">
              <FlaskConical size={18} className="text-secondary/50" />
            </div>
            <div>
              <p className="text-xs font-medium text-text-secondary mb-0.5">Agent Playground</p>
              <p className="text-[10px] text-text-muted">
                {resolvedAuth.level === 'none'
                  ? 'Configure an API key in Vault to start chatting'
                  : 'Send a message to start a test flight session'}
              </p>
            </div>
          </div>
        ) : (
          <div className="space-y-0.5">
            {messages.map((msg) =>
              msg.role === 'session-start' ? (
                <SessionDivider key={msg.id} timestamp={msg.timestamp} />
              ) : (
                <MessageBubble key={msg.id} msg={msg} />
              )
            )}
            <div ref={messagesEndRef} />
          </div>
        )}
      </div>

      {/* Reasoning waterfall — tool call timeline */}
      <ReasoningWaterfall />

      {/* Inline error */}
      {error && (
        <div className="flex items-center gap-2 px-4 py-2 bg-status-error/6 border-t border-status-error/15 text-[10px] text-status-error flex-shrink-0">
          <AlertCircle size={11} />
          {error}
        </div>
      )}

      {/* Input area */}
      <div className="flex-shrink-0 border-t border-border/30 bg-surface/40 px-3 py-2">
        {/* Model selector row */}
        <div className="flex items-center gap-2 mb-2">
          <ModelSelector
            selectedModel={selectedModel}
            ollamaEndpoint={ollamaEndpoint}
            onSelectModel={setSelectedModel}
            onOllamaEndpointChange={setOllamaEndpoint}
            defaultLabel={resolvedAuth.model}
          />
          {selectedModel && (
            <span className="text-[9px] text-text-muted/50">
              Override active
            </span>
          )}
        </div>
        <div
          className={cn(
            'flex items-end gap-2 rounded-lg border transition-colors',
            'bg-background/60',
            canSend || input.trim()
              ? 'border-border/60 focus-within:border-secondary/40'
              : 'border-border/30'
          )}
        >
          <textarea
            ref={textareaRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={
              resolvedAuth.level === 'none'
                ? 'Configure auth to send messages…'
                : 'Message… (Enter to send, Shift+Enter for newline)'
            }
            disabled={resolvedAuth.level === 'none' || isLoading}
            rows={1}
            className={cn(
              'flex-1 bg-transparent resize-none text-xs text-text-primary',
              'placeholder:text-text-muted/50 focus:outline-none',
              'px-3 py-2.5 min-h-[36px] max-h-[120px] overflow-y-auto',
              'disabled:opacity-40 disabled:cursor-not-allowed',
              'leading-relaxed'
            )}
            style={{ fieldSizing: 'content' } as React.CSSProperties}
          />
          <div className="flex items-center gap-1 px-2 pb-1.5 flex-shrink-0">
            {messages.length > 0 && (
              <IconButton
                icon={Trash2}
                onClick={clearMessages}
                tooltip="Clear conversation"
                size="sm"
                variant="ghost"
              />
            )}
            <button
              onClick={sendMessage}
              disabled={!canSend}
              aria-label="Send message"
              className={cn(
                'w-7 h-7 rounded-md flex items-center justify-center transition-all',
                canSend
                  ? 'bg-secondary/15 border border-secondary/25 text-secondary hover:bg-secondary/25 hover:border-secondary/40'
                  : 'bg-surface-elevated/40 border border-border/20 text-text-muted/30 cursor-not-allowed'
              )}
            >
              {isLoading ? (
                <span className="w-3 h-3 rounded-full border-2 border-secondary/30 border-t-secondary animate-spin" />
              ) : (
                <Send size={11} />
              )}
            </button>
          </div>
        </div>
        <p className="text-[9px] text-text-muted/40 mt-1 px-1">
          Enter to send · Shift+Enter for newline · conversation persists per session
        </p>
      </div>
    </div>
  );
}
