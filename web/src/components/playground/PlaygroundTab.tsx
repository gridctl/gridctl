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
} from 'lucide-react';
import { marked } from 'marked';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { useUIStore } from '../../stores/useUIStore';
import { usePlaygroundStore } from '../../stores/usePlaygroundStore';
import {
  fetchPlaygroundAuth,
  sendPlaygroundChat,
  buildStreamHeaders,
  type PlaygroundAuthResponse,
} from '../../lib/api';

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

// ─── Message bubble ───────────────────────────────────────────────────────────

function MessageBubble({ msg }: { msg: { id: string; role: string; content: string; timestamp: string; isStreaming?: boolean; error?: boolean } }) {
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
      <div
        className={cn(
          'max-w-[75%] rounded-lg px-3 py-2 text-xs leading-relaxed',
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
    </div>
  );
}

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
  const addMessage = usePlaygroundStore((s) => s.addMessage);
  const updateStreamingMessage = usePlaygroundStore((s) => s.updateStreamingMessage);
  const finalizeMessage = usePlaygroundStore((s) => s.finalizeMessage);
  const setLoading = usePlaygroundStore((s) => s.setLoading);
  const setError = usePlaygroundStore((s) => s.setError);
  const setAuthStatus = usePlaygroundStore((s) => s.setAuthStatus);
  const setAuthLoading = usePlaygroundStore((s) => s.setAuthLoading);
  const clearMessages = usePlaygroundStore((s) => s.clearMessages);

  const [input, setInput] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const streamingMsgIdRef = useRef<string | null>(null);
  const streamContentRef = useRef<string>('');
  const sseControllerRef = useRef<AbortController | null>(null);

  const resolvedAuth = resolveAuth(authStatus);
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

            let event: { type: string; data?: { text?: string; message?: string } };
            try {
              event = JSON.parse(raw);
            } catch {
              continue;
            }

            const msgId = streamingMsgIdRef.current;
            if (!msgId) continue;

            switch (event.type) {
              case 'token':
                streamContentRef.current += event.data?.text ?? '';
                updateStreamingMessage(msgId, streamContentRef.current);
                break;

              case 'done':
                finalizeMessage(msgId, streamContentRef.current);
                streamingMsgIdRef.current = null;
                streamContentRef.current = '';
                setLoading(false);
                sseControllerRef.current?.abort();
                return;

              case 'error':
                finalizeMessage(msgId, event.data?.message ?? 'Inference failed', true);
                streamingMsgIdRef.current = null;
                streamContentRef.current = '';
                setLoading(false);
                sseControllerRef.current?.abort();
                return;
            }
          }
        }
      } catch {
        // AbortError or read failure — clean up if we were still streaming
        const msgId = streamingMsgIdRef.current;
        if (msgId) {
          finalizeMessage(msgId, streamContentRef.current || '', true);
          streamingMsgIdRef.current = null;
          streamContentRef.current = '';
          setLoading(false);
        }
      }
    },
    [sessionId, updateStreamingMessage, finalizeMessage, setLoading]
  );

  // ── Send message ────────────────────────────────────────────────────────

  const sendMessage = useCallback(async () => {
    const text = input.trim();
    if (!text || isLoading || resolvedAuth.level === 'none') return;

    setInput('');
    setError(null);

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
    addMessage({
      id: assistantId,
      role: 'assistant',
      content: '',
      timestamp: new Date().toISOString(),
      isStreaming: true,
    });

    setLoading(true);

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
        authMode: resolvedAuth.authMode,
        model: resolvedAuth.model,
        ...(resolvedAuth.ollamaUrl ? { ollamaUrl: resolvedAuth.ollamaUrl } : {}),
      });
    } catch (err: unknown) {
      controller.abort();
      setLoading(false);
      finalizeMessage(assistantId, '', true);
      streamingMsgIdRef.current = null;
      setError(err instanceof Error ? err.message : 'Failed to send message');
    }
  }, [
    input,
    isLoading,
    resolvedAuth,
    sessionId,
    addMessage,
    finalizeMessage,
    setLoading,
    setError,
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
            {messages.map((msg) => (
              <MessageBubble key={msg.id} msg={msg} />
            ))}
            <div ref={messagesEndRef} />
          </div>
        )}
      </div>

      {/* Inline error */}
      {error && (
        <div className="flex items-center gap-2 px-4 py-2 bg-status-error/6 border-t border-status-error/15 text-[10px] text-status-error flex-shrink-0">
          <AlertCircle size={11} />
          {error}
        </div>
      )}

      {/* Input area */}
      <div className="flex-shrink-0 border-t border-border/30 bg-surface/40 px-3 py-2">
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
