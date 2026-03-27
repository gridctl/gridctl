import React, { useState, useEffect, useRef, useCallback } from 'react';
import { X, Bot, Settings, Wrench, Eye, Rocket, Save, Check, Link } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import type { AgentStatus, ToolSelector } from '../../types';

interface AgentBuilderInspectorProps {
  agentId: string;
  onClose: () => void;
}

type InspectorTab = 'config' | 'tools' | 'preview';

interface VaultKey {
  key: string;
  set?: string;
}

interface ToolEntry {
  serverName: string;
  toolName: string;
  enabled: boolean;
}

// Serialize agent config to a YAML preview string.
function serializeAgentYAML(
  agent: AgentStatus,
  prompt: string,
  toolEntries: ToolEntry[],
  a2aPeers?: string[],
): string {
  const lines: string[] = [];

  lines.push(`name: ${agent.name}`);
  if (agent.image) lines.push(`image: ${agent.image}`);
  if (agent.description) lines.push(`description: "${agent.description}"`);
  if (agent.variant === 'remote' && agent.url) lines.push(`url: ${agent.url}`);

  if (prompt.trim()) {
    lines.push('prompt: |');
    for (const line of prompt.split('\n')) {
      lines.push(`  ${line}`);
    }
  }

  // Build uses from enabled tools, grouped by server
  const serverMap = new Map<string, string[]>();
  for (const t of toolEntries) {
    if (!t.enabled) continue;
    if (!serverMap.has(t.serverName)) serverMap.set(t.serverName, []);
    serverMap.get(t.serverName)!.push(t.toolName);
  }

  // Also include servers that are in uses but have no individual tools listed
  // (meaning all tools are included) — those come through as empty entries
  const allServersInUses = new Set<string>((agent.uses ?? []).map((u) => u.server));

  const finalUses: { server: string; tools?: string[] }[] = [];
  for (const serverName of allServersInUses) {
    const enabledTools = serverMap.get(serverName) ?? [];
    // Check if all tools from this server are enabled
    const allToolsForServer = toolEntries.filter((t) => t.serverName === serverName);
    const allEnabled = allToolsForServer.length === 0 || allToolsForServer.every((t) => t.enabled);
    if (allEnabled) {
      finalUses.push({ server: serverName });
    } else if (enabledTools.length > 0) {
      finalUses.push({ server: serverName, tools: enabledTools });
    }
  }

  if (finalUses.length > 0) {
    lines.push('uses:');
    for (const u of finalUses) {
      if (!u.tools || u.tools.length === 0) {
        lines.push(`  - server: ${u.server}`);
      } else {
        lines.push(`  - server: ${u.server}`);
        lines.push(`    tools:`);
        for (const t of u.tools) {
          lines.push(`      - ${t}`);
        }
      }
    }
  }

  // A2A peer agents go in equipped_skills
  if (a2aPeers && a2aPeers.length > 0) {
    lines.push('equipped_skills:');
    for (const peer of a2aPeers) {
      lines.push(`  - server: ${peer}`);
    }
  }

  return lines.join('\n');
}

// Build ToolSelector[] from entries for the save request.
function buildUsesFromEntries(
  agent: AgentStatus,
  toolEntries: ToolEntry[],
): ToolSelector[] {
  const serverMap = new Map<string, string[]>();
  for (const t of toolEntries) {
    if (!t.enabled) continue;
    if (!serverMap.has(t.serverName)) serverMap.set(t.serverName, []);
    serverMap.get(t.serverName)!.push(t.toolName);
  }

  const allServersInUses = (agent.uses ?? []).map((u) => u.server);
  const result: ToolSelector[] = [];

  for (const serverName of allServersInUses) {
    const enabledTools = serverMap.get(serverName) ?? [];
    const allToolsForServer = toolEntries.filter((t) => t.serverName === serverName);
    const allEnabled = allToolsForServer.length === 0 || allToolsForServer.every((t) => t.enabled);
    if (allEnabled) {
      result.push({ server: serverName });
    } else if (enabledTools.length > 0) {
      result.push({ server: serverName, tools: enabledTools });
    }
  }

  return result;
}

// Render syntax-highlighted YAML with amber vault vars and blue env vars.
function HighlightedText({ text }: { text: string }) {
  const parts: React.ReactNode[] = [];
  const regex = /(\$\{vault:[^}]+\}|\$\{[^}]+\}|\$[A-Z_][A-Z0-9_]*)/g;
  let last = 0;
  let m: RegExpExecArray | null;

  while ((m = regex.exec(text)) !== null) {
    if (m.index > last) {
      parts.push(text.slice(last, m.index));
    }
    const token = m[0];
    const isVault = token.startsWith('${vault:');
    parts.push(
      <span
        key={m.index}
        className={cn(
          'rounded px-0.5',
          isVault ? 'text-amber-400 bg-amber-400/10' : 'text-blue-400 bg-blue-400/10',
        )}
      >
        {token}
      </span>,
    );
    last = m.index + token.length;
  }

  if (last < text.length) parts.push(text.slice(last));
  return <>{parts}</>;
}

export function AgentBuilderInspector({ agentId, onClose }: AgentBuilderInspectorProps) {
  const [tab, setTab] = useState<InspectorTab>('config');
  const [draftPrompt, setDraftPrompt] = useState('');
  const [toolEntries, setToolEntries] = useState<ToolEntry[]>([]);
  const [vaultKeys, setVaultKeys] = useState<VaultKey[]>([]);
  const [vaultDropdownOpen, setVaultDropdownOpen] = useState(false);
  const [vaultQuery, setVaultQuery] = useState('');
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const agents = useStackStore((s) => s.agents);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const draftEquippedSkills = useStackStore((s) => s.draftEquippedSkills);
  const setBottomPanelTab = useUIStore((s) => s.setBottomPanelTab);

  const agent = agents.find((a) => a.name === agentId);

  // Initialize draft state from agent data.
  useEffect(() => {
    if (!agent) return;
    setDraftPrompt(agent.uses ? '' : '');

    // Fetch raw prompt from the backend (raw, unexpanded)
    fetch(`/api/stack/spec`)
      .then((r) => r.json())
      .then((spec: { content: string }) => {
        // Parse the YAML text to extract the prompt for this agent
        const content = spec.content;
        const agentSection = parseAgentPromptFromYAML(content, agentId);
        setDraftPrompt(agentSection);
      })
      .catch(() => {
        // Fallback: empty prompt
        setDraftPrompt('');
      });
  }, [agentId, agent]);

  // Build tool entries from agent's uses and connected servers.
  useEffect(() => {
    if (!agent) return;

    const uses = agent.uses ?? [];
    const entries: ToolEntry[] = [];

    for (const selector of uses) {
      const server = mcpServers.find((s) => s.name === selector.server);
      if (!server) continue;

      const tools = server.tools ?? [];
      const allowedTools = selector.tools ?? []; // empty = all allowed

      for (const toolName of tools) {
        const enabled = allowedTools.length === 0 || allowedTools.includes(toolName);
        entries.push({ serverName: server.name, toolName, enabled });
      }
    }

    setToolEntries(entries);
  }, [agent, mcpServers]);

  // Fetch vault keys for autocomplete.
  useEffect(() => {
    fetch('/api/vault')
      .then((r) => r.json())
      .then((keys: VaultKey[]) => setVaultKeys(Array.isArray(keys) ? keys : []))
      .catch(() => setVaultKeys([]));
  }, []);

  // Close vault dropdown when clicking outside.
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setVaultDropdownOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, []);

  const handlePromptChange = useCallback((e: React.ChangeEvent<HTMLTextAreaElement>) => {
    const value = e.target.value;
    setDraftPrompt(value);
    setSaveSuccess(false);

    // Detect ${vault: trigger for autocomplete
    const cursor = e.target.selectionStart ?? 0;
    const before = value.slice(0, cursor);
    const match = before.match(/\$\{vault:([^}]*)$/);
    if (match) {
      setVaultQuery(match[1]);
      setVaultDropdownOpen(true);
    } else {
      setVaultDropdownOpen(false);
      setVaultQuery('');
    }
  }, []);

  const insertVaultKey = useCallback((keyName: string) => {
    const textarea = textareaRef.current;
    if (!textarea) return;

    const cursor = textarea.selectionStart ?? 0;
    const value = textarea.value;
    const before = value.slice(0, cursor);
    const after = value.slice(cursor);

    // Find the start of the ${vault: sequence
    const triggerStart = before.lastIndexOf('${vault:');
    if (triggerStart === -1) return;

    const newValue = before.slice(0, triggerStart) + `\${vault:${keyName}}` + after;
    setDraftPrompt(newValue);
    setVaultDropdownOpen(false);
    setVaultQuery('');

    // Restore focus and move cursor after the inserted expression
    setTimeout(() => {
      textarea.focus();
      const newCursor = triggerStart + `\${vault:${keyName}}`.length;
      textarea.setSelectionRange(newCursor, newCursor);
    }, 0);
  }, []);

  const handleToolToggle = useCallback((serverName: string, toolName: string) => {
    setToolEntries((prev) =>
      prev.map((t) =>
        t.serverName === serverName && t.toolName === toolName
          ? { ...t, enabled: !t.enabled }
          : t,
      ),
    );
    setSaveSuccess(false);
  }, []);

  const handleSave = useCallback(async () => {
    if (!agent) return;
    setSaving(true);
    setSaveError(null);
    setSaveSuccess(false);

    try {
      const uses = buildUsesFromEntries(agent, toolEntries);
      const peers = Array.from(draftEquippedSkills.get(agent.name) ?? []);
      const equippedSkills = peers.map((p) => ({ server: p }));
      const res = await fetch('/api/playground/agent', {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          agentId: agent.name,
          prompt: draftPrompt,
          uses,
          ...(equippedSkills.length > 0 ? { equippedSkills } : {}),
        }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: 'Save failed' }));
        throw new Error(err.error ?? 'Save failed');
      }

      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2500);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }, [agent, draftPrompt, toolEntries, draftEquippedSkills]);

  const filteredVaultKeys = vaultKeys.filter((k) =>
    k.key.toLowerCase().includes(vaultQuery.toLowerCase()),
  );

  const a2aPeers = agent ? Array.from(draftEquippedSkills.get(agent.name) ?? []) : [];
  const yamlPreview = agent
    ? serializeAgentYAML(agent, draftPrompt, toolEntries, a2aPeers)
    : '';

  // Group tool entries by server for the tools tab.
  const toolsByServer = toolEntries.reduce<Record<string, ToolEntry[]>>((acc, t) => {
    if (!acc[t.serverName]) acc[t.serverName] = [];
    acc[t.serverName].push(t);
    return acc;
  }, {});

  if (!agent) return null;

  return (
    <div
      className={cn(
        'absolute top-0 right-0 bottom-0 w-[400px] z-30',
        'flex flex-col',
        'glass-panel-elevated border-l border-border/50',
        'bg-surface/95 backdrop-blur-xl',
        'transition-transform duration-300',
      )}
    >
      {/* Header */}
      <div className="flex-shrink-0 h-12 flex items-center justify-between px-4 border-b border-border/40">
        <div className="flex items-center gap-2.5">
          <div className="p-1.5 rounded-md bg-tertiary/10 border border-tertiary/30">
            <Bot size={13} className="text-tertiary" />
          </div>
          <div>
            <div className="text-xs font-semibold text-text-primary">{agent.name}</div>
            <div className="text-[10px] text-text-muted">Agent Builder</div>
          </div>
        </div>
        <button
          onClick={onClose}
          className="p-1 rounded-md hover:bg-surface-highlight text-text-muted hover:text-text-primary transition-colors"
          title="Close inspector"
        >
          <X size={14} />
        </button>
      </div>

      {/* Tabs */}
      <div className="flex-shrink-0 flex border-b border-border/40 px-2">
        {([
          { id: 'config' as const, label: 'Config', icon: Settings },
          { id: 'tools' as const, label: 'Tools', icon: Wrench },
          { id: 'preview' as const, label: 'Preview', icon: Eye },
        ] as { id: InspectorTab; label: string; icon: React.ElementType }[]).map((t) => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            className={cn(
              'flex items-center gap-1.5 px-3 py-2.5 text-[11px] font-medium transition-colors relative',
              tab === t.id
                ? 'text-text-primary'
                : 'text-text-muted hover:text-text-secondary',
            )}
          >
            <t.icon size={11} />
            {t.label}
            {tab === t.id && (
              <span className="absolute bottom-0 left-1 right-1 h-0.5 bg-tertiary rounded-full" />
            )}
          </button>
        ))}
      </div>

      {/* Tab content */}
      <div className="flex-1 min-h-0 overflow-y-auto">
        {/* Config tab */}
        {tab === 'config' && (
          <div className="p-4 space-y-4">
            <div>
              <label className="block text-[10px] font-medium text-text-muted uppercase tracking-wider mb-2">
                System Prompt
              </label>
              <div className="relative" ref={dropdownRef}>
                {/* Syntax highlight overlay (behind textarea) */}
                <pre
                  aria-hidden
                  className={cn(
                    'absolute inset-0 p-2.5 text-[11px] font-mono leading-relaxed',
                    'text-transparent pointer-events-none whitespace-pre-wrap break-words',
                    'overflow-hidden',
                  )}
                >
                  <HighlightedText text={draftPrompt + ' '} />
                </pre>
                <textarea
                  ref={textareaRef}
                  value={draftPrompt}
                  onChange={handlePromptChange}
                  placeholder="Enter system prompt… Use ${vault:KEY} for vault variables and ${ENV_VAR} for environment variables."
                  rows={10}
                  className={cn(
                    'relative w-full bg-surface-elevated border border-border/40 rounded-lg',
                    'p-2.5 text-[11px] font-mono leading-relaxed text-text-primary',
                    'resize-y min-h-[120px] outline-none',
                    'focus:border-tertiary/50 focus:ring-1 focus:ring-tertiary/20',
                    'placeholder:text-text-muted/40 transition-colors',
                    // Make textarea caret visible but text transparent so highlight shows through
                    'caret-text-primary',
                  )}
                  style={{ caretColor: 'var(--color-text-primary)' }}
                  spellCheck={false}
                />

                {/* Vault autocomplete dropdown */}
                {vaultDropdownOpen && filteredVaultKeys.length > 0 && (
                  <div className="absolute left-0 right-0 z-50 mt-1 glass-panel rounded-lg border border-amber-500/30 overflow-hidden shadow-lg">
                    <div className="px-2.5 py-1.5 border-b border-border/30 flex items-center gap-1.5">
                      <span className="text-[9px] text-amber-400 uppercase tracking-wider font-medium">
                        Vault Keys
                      </span>
                    </div>
                    <div className="max-h-48 overflow-y-auto">
                      {filteredVaultKeys.map((k) => (
                        <button
                          key={k.key}
                          onMouseDown={(e) => {
                            e.preventDefault();
                            insertVaultKey(k.key);
                          }}
                          className="w-full flex items-center justify-between px-2.5 py-1.5 hover:bg-amber-500/10 transition-colors text-left"
                        >
                          <span className="text-[11px] font-mono text-amber-400">{k.key}</span>
                          {k.set && (
                            <span className="text-[9px] text-text-muted">{k.set}</span>
                          )}
                        </button>
                      ))}
                    </div>
                    <button
                      onMouseDown={(e) => {
                        e.preventDefault();
                        setVaultDropdownOpen(false);
                      }}
                      className="w-full px-2.5 py-1 text-[9px] text-text-muted hover:text-text-secondary border-t border-border/30 text-left transition-colors"
                    >
                      Press Esc to dismiss
                    </button>
                  </div>
                )}
              </div>
              <p className="mt-1.5 text-[9px] text-text-muted">
                Type <span className="text-amber-400 font-mono">{'${vault:'}</span> to autocomplete vault keys.
                Use <span className="text-blue-400 font-mono">{'${ENV_VAR}'}</span> for environment variables.
              </p>
            </div>

            {/* Test Flight shortcut */}
            <button
              onClick={() => setBottomPanelTab('playground')}
              className={cn(
                'w-full flex items-center justify-center gap-2 py-2 px-3 rounded-lg text-xs font-medium',
                'bg-primary/10 text-primary border border-primary/20',
                'hover:bg-primary/20 transition-colors',
              )}
            >
              <Rocket size={12} />
              Test Flight ›
            </button>
          </div>
        )}

        {/* Tools tab */}
        {tab === 'tools' && (
          <div className="p-4 space-y-4">
            {/* A2A peers wired via Agent Builder Mode */}
            {a2aPeers.length > 0 && (
              <div className="space-y-1">
                <div className="flex items-center justify-between mb-1.5">
                  <span className="text-[10px] font-medium text-tertiary/80 uppercase tracking-wider flex items-center gap-1.5">
                    <Link size={9} />
                    A2A Peers
                  </span>
                  <span className="text-[9px] text-text-muted">agent-to-agent</span>
                </div>
                <div className="space-y-0.5">
                  {a2aPeers.map((peer) => (
                    <div
                      key={peer}
                      className="flex items-center gap-2.5 px-2.5 py-1.5 rounded-md bg-tertiary/5 border border-tertiary/15"
                    >
                      <Bot size={10} className="text-tertiary flex-shrink-0" />
                      <span className="text-[11px] font-mono text-text-primary">{peer}</span>
                      <span className="ml-auto text-[9px] text-tertiary/60">equipped_skill</span>
                    </div>
                  ))}
                </div>
                <p className="text-[9px] text-text-muted/50 mt-1">
                  Wired via Agent Builder Mode. Saved to <code className="font-mono">equipped_skills</code>.
                </p>
              </div>
            )}
            {Object.keys(toolsByServer).length === 0 && a2aPeers.length === 0 ? (
              <div className="text-center py-8">
                <Wrench size={20} className="text-text-muted mx-auto mb-2 opacity-50" />
                <p className="text-[11px] text-text-muted">
                  No MCP servers connected to this agent.
                </p>
                <p className="text-[10px] text-text-muted/60 mt-1">
                  Add connections via Wiring Mode or edit the stack YAML.
                </p>
              </div>
            ) : Object.keys(toolsByServer).length === 0 ? null : (
              Object.entries(toolsByServer).map(([serverName, tools]) => {
                const enabledCount = tools.filter((t) => t.enabled).length;
                const totalCount = tools.length;

                return (
                  <div key={serverName} className="space-y-1">
                    <div className="flex items-center justify-between mb-1.5">
                      <span className="text-[10px] font-medium text-text-secondary uppercase tracking-wider">
                        {serverName}
                      </span>
                      <span className="text-[9px] text-text-muted">
                        {enabledCount}/{totalCount}
                      </span>
                    </div>
                    <div className="space-y-0.5">
                      {tools.map((t) => (
                        <button
                          key={t.toolName}
                          onClick={() => handleToolToggle(t.serverName, t.toolName)}
                          className={cn(
                            'w-full flex items-center gap-2.5 px-2.5 py-1.5 rounded-md text-left transition-all duration-150',
                            t.enabled
                              ? 'bg-tertiary/5 hover:bg-tertiary/10 border border-tertiary/15'
                              : 'hover:bg-surface-highlight/40 border border-transparent',
                          )}
                        >
                          {/* Checkbox */}
                          <div
                            className={cn(
                              'w-3.5 h-3.5 rounded border flex-shrink-0 flex items-center justify-center transition-all',
                              t.enabled
                                ? 'bg-tertiary border-tertiary/60'
                                : 'border-border/50 bg-transparent',
                            )}
                          >
                            {t.enabled && <Check size={9} className="text-background" />}
                          </div>
                          <div className="min-w-0">
                            <div
                              className={cn(
                                'text-[11px] font-mono truncate',
                                t.enabled ? 'text-text-primary' : 'text-text-muted',
                              )}
                            >
                              {t.toolName}
                            </div>
                            <div className="text-[9px] text-text-muted/60 truncate">
                              {serverName}
                            </div>
                          </div>
                        </button>
                      ))}
                    </div>
                  </div>
                );
              })
            )}
          </div>
        )}

        {/* Preview tab */}
        {tab === 'preview' && (
          <div className="p-4">
            <div className="flex items-center justify-between mb-2">
              <span className="text-[10px] font-medium text-text-muted uppercase tracking-wider">
                YAML Preview
              </span>
              <span className="text-[9px] text-text-muted/60">Live — updates as you edit</span>
            </div>
            <pre
              className={cn(
                'w-full rounded-lg p-3 overflow-x-auto',
                'bg-surface-elevated border border-border/30',
                'text-[11px] font-mono text-text-secondary leading-relaxed',
                'whitespace-pre-wrap break-words',
              )}
            >
              <HighlightedText text={yamlPreview} />
            </pre>
          </div>
        )}
      </div>

      {/* Footer — Save to Stack */}
      <div className="flex-shrink-0 border-t border-border/40 px-4 py-3">
        {saveError && (
          <p className="text-[10px] text-red-400 mb-2 truncate">{saveError}</p>
        )}
        <button
          onClick={handleSave}
          disabled={saving}
          className={cn(
            'w-full flex items-center justify-center gap-2 py-2 px-3 rounded-lg text-xs font-medium',
            'transition-all duration-200',
            saveSuccess
              ? 'bg-secondary/20 text-secondary border border-secondary/30'
              : 'bg-tertiary/15 text-tertiary border border-tertiary/25 hover:bg-tertiary/25',
            saving && 'opacity-60 cursor-not-allowed',
          )}
        >
          {saveSuccess ? (
            <>
              <Check size={12} />
              Saved
            </>
          ) : (
            <>
              <Save size={12} />
              {saving ? 'Saving…' : 'Save to Stack'}
            </>
          )}
        </button>
      </div>
    </div>
  );
}

// parseAgentPromptFromYAML extracts the raw prompt field for a named agent from YAML text.
// Returns empty string if not found.
function parseAgentPromptFromYAML(yaml: string, agentName: string): string {
  const lines = yaml.split('\n');
  let inAgents = false;
  let inTargetAgent = false;
  let inPrompt = false;
  let promptIndent = 0;
  const promptLines: string[] = [];
  let blockScalar = false;

  for (const line of lines) {
    if (line.match(/^agents:/)) {
      inAgents = true;
      continue;
    }
    if (!inAgents) continue;

    // Detect a new top-level section (non-indented key)
    if (inAgents && !line.startsWith(' ') && !line.startsWith('\t') && line.trim() !== '' && !line.trim().startsWith('-')) {
      if (inTargetAgent) break;
      inAgents = false;
      continue;
    }

    if (inAgents && !inTargetAgent) {
      const nameMatch = line.match(/^\s+- name:\s+(.+)$/);
      if (nameMatch && nameMatch[1].trim() === agentName) {
        inTargetAgent = true;
      }
      continue;
    }

    if (inTargetAgent && !inPrompt) {
      // Check if we've moved to a new agent entry
      if (line.match(/^\s+- name:/)) break;
      const promptMatch = line.match(/^(\s+)prompt:\s*(.*)/);
      if (promptMatch) {
        inPrompt = true;
        promptIndent = promptMatch[1].length;
        const rest = promptMatch[2].trim();
        if (rest === '|' || rest === '>') {
          blockScalar = true;
        } else if (rest) {
          return rest.replace(/^["']|["']$/g, '');
        }
      }
      continue;
    }

    if (inPrompt && blockScalar) {
      const lineIndent = line.match(/^(\s*)/)?.[1].length ?? 0;
      if (line.trim() === '' || lineIndent > promptIndent) {
        // Strip the extra indentation
        promptLines.push(line.slice(promptIndent + 2));
      } else {
        break;
      }
    }
  }

  return promptLines.join('\n').trimEnd();
}
