import { useState, useMemo, useCallback } from 'react';
import {
  Search,
  ChevronDown,
  ChevronRight,
  Wrench,
  Plus,
  X,
  Server,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import type { Tool, SkillInput, WorkflowOutput } from '../../types';

interface ToolboxPaletteProps {
  tools: Tool[];
  inputs: Record<string, SkillInput>;
  output: WorkflowOutput | undefined;
  onInputsChange: (inputs: Record<string, SkillInput>) => void;
  onOutputChange: (output: WorkflowOutput | undefined) => void;
  stepIds: string[];
}

// Group tools by server prefix (everything before __)
function groupTools(tools: Tool[]): Record<string, Tool[]> {
  const groups: Record<string, Tool[]> = {};
  for (const tool of tools ?? []) {
    const parts = tool.name.split('__');
    const server = parts.length > 1 ? parts[0] : 'other';
    if (!groups[server]) groups[server] = [];
    groups[server].push(tool);
  }
  return groups;
}

function ToolItem({ tool }: { tool: Tool }) {
  const toolName = tool.name;
  return (
    <div
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData('application/reactflow', JSON.stringify({ tool: toolName }));
        e.dataTransfer.effectAllowed = 'move';
      }}
      title={tool.description ?? toolName}
      className={cn(
        'px-3 py-1.5 text-xs font-mono text-text-secondary cursor-grab',
        'hover:bg-surface-highlight hover:text-text-primary transition-all duration-200',
        'border-l-2 border-transparent hover:border-primary/40',
      )}
    >
      <Wrench size={10} className="inline mr-2 text-text-muted" />
      {toolName.split('__').pop()}
    </div>
  );
}

function InputEditor({
  inputs,
  onChange,
}: {
  inputs: Record<string, SkillInput>;
  onChange: (inputs: Record<string, SkillInput>) => void;
}) {
  const entries = Object.entries(inputs);

  const addInput = useCallback(() => {
    const name = `input-${entries.length + 1}`;
    onChange({ ...inputs, [name]: { type: 'string', required: false } });
  }, [inputs, entries.length, onChange]);

  const removeInput = useCallback(
    (name: string) => {
      const next = { ...inputs };
      delete next[name];
      onChange(next);
    },
    [inputs, onChange],
  );

  const updateInput = useCallback(
    (oldName: string, newName: string, input: SkillInput) => {
      const next: Record<string, SkillInput> = {};
      for (const [k, v] of Object.entries(inputs)) {
        if (k === oldName) {
          next[newName] = input;
        } else {
          next[k] = v;
        }
      }
      onChange(next);
    },
    [inputs, onChange],
  );

  return (
    <div>
      <div className="flex items-center justify-between px-3 py-2">
        <span className="text-[10px] text-text-muted uppercase tracking-wider">Inputs</span>
        <button
          onClick={addInput}
          className="text-xs text-primary hover:text-primary/80 flex items-center gap-0.5 transition-colors"
        >
          <Plus size={10} /> Add
        </button>
      </div>
      {entries.length === 0 && (
        <p className="px-3 text-xs text-text-muted/40 italic">No inputs defined</p>
      )}
      <div className="space-y-1 px-2">
        {entries.map(([name, input]) => (
          <div key={name} className="flex items-center gap-1 group">
            <input
              value={name}
              onChange={(e) => updateInput(name, e.target.value, input)}
              className="w-20 bg-transparent border-b border-border/20 text-xs font-mono text-text-secondary px-1 py-0.5 focus:outline-none focus:border-primary/50"
            />
            <select
              value={input.type}
              onChange={(e) => updateInput(name, name, { ...input, type: e.target.value })}
              className="bg-transparent text-xs text-text-muted focus:outline-none cursor-pointer"
            >
              <option value="string">string</option>
              <option value="number">number</option>
              <option value="boolean">boolean</option>
              <option value="object">object</option>
              <option value="array">array</option>
            </select>
            <button
              onClick={() => input.required ? updateInput(name, name, { ...input, required: false }) : updateInput(name, name, { ...input, required: true })}
              className={cn('text-[9px] px-1 rounded', input.required ? 'text-primary bg-primary/10' : 'text-text-muted/40')}
            >
              req
            </button>
            <button
              onClick={() => removeInput(name)}
              className="p-0.5 text-text-muted/30 hover:text-status-error opacity-0 group-hover:opacity-100 transition-all"
            >
              <X size={10} />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}

function OutputEditor({
  output,
  onChange,
  stepIds,
}: {
  output: WorkflowOutput | undefined;
  onChange: (output: WorkflowOutput | undefined) => void;
  stepIds: string[];
}) {
  const format = output?.format ?? 'merged';

  return (
    <div className="px-3 py-2">
      <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-2">Output</span>
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <span className="text-xs text-text-muted">Format:</span>
          <select
            value={format}
            onChange={(e) => onChange({ ...output, format: e.target.value })}
            className="bg-background/60 border border-border/40 rounded px-2 py-0.5 text-xs text-text-primary focus:outline-none focus:border-primary/50"
          >
            <option value="merged">merged</option>
            <option value="last">last</option>
            <option value="custom">custom</option>
          </select>
        </div>
        {format === 'merged' && (
          <div>
            <span className="text-[10px] text-text-muted block mb-1">Include steps:</span>
            <div className="flex flex-wrap gap-1">
              {(stepIds ?? []).map((id) => {
                const included = (output?.include ?? []).includes(id);
                return (
                  <button
                    key={id}
                    onClick={() => {
                      const current = output?.include ?? [];
                      const next = included ? current.filter((s) => s !== id) : [...current, id];
                      onChange({ ...output, format: 'merged', include: next.length > 0 ? next : undefined });
                    }}
                    className={cn(
                      'text-[10px] font-mono px-1.5 py-0.5 rounded border transition-all',
                      included ? 'text-primary border-primary/30 bg-primary/10' : 'text-text-muted border-border/30',
                    )}
                  >
                    {id}
                  </button>
                );
              })}
            </div>
          </div>
        )}
        {format === 'custom' && (
          <textarea
            value={output?.template ?? ''}
            onChange={(e) => onChange({ ...output, format: 'custom', template: e.target.value })}
            placeholder="Custom output template..."
            rows={3}
            className="w-full bg-background/60 border border-border/40 rounded px-2 py-1 text-xs font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 resize-y"
          />
        )}
      </div>
    </div>
  );
}

export function ToolboxPalette({
  tools,
  inputs,
  output,
  onInputsChange,
  onOutputChange,
  stepIds,
}: ToolboxPaletteProps) {
  const [search, setSearch] = useState('');
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  const grouped = useMemo(() => groupTools(tools), [tools]);

  const filtered = useMemo(() => {
    if (!search) return grouped;
    const lowerSearch = search.toLowerCase();
    const result: Record<string, Tool[]> = {};
    for (const [server, serverTools] of Object.entries(grouped)) {
      const matching = (serverTools ?? []).filter(
        (t) => t.name.toLowerCase().includes(lowerSearch) || (t.description ?? '').toLowerCase().includes(lowerSearch),
      );
      if (matching.length > 0) result[server] = matching;
    }
    return result;
  }, [grouped, search]);

  return (
    <div className="w-[220px] bg-surface border-r border-border/50 flex flex-col h-full overflow-hidden flex-shrink-0">
      {/* Header */}
      <div className="px-3 py-2.5 border-b border-border/30 flex-shrink-0">
        <span className="text-[10px] text-text-muted uppercase tracking-wider">Tools</span>
      </div>

      {/* Search */}
      <div className="px-2 py-1.5 border-b border-border/20 flex-shrink-0">
        <div className="relative">
          <Search size={12} className="absolute left-2 top-1/2 -translate-y-1/2 text-text-muted/50" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search tools..."
            className="w-full bg-background/40 border border-border/30 rounded-lg pl-7 pr-2 py-1 text-xs text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/40"
          />
        </div>
      </div>

      {/* Tool groups */}
      <div className="flex-1 overflow-y-auto scrollbar-dark min-h-0">
        {Object.entries(filtered).map(([server, serverTools]) => (
          <div key={server}>
            <button
              onClick={() => setCollapsed((prev) => ({ ...prev, [server]: !prev[server] }))}
              className="w-full flex items-center gap-1.5 px-3 py-1.5 text-xs text-violet-400 hover:bg-violet-500/5 transition-colors"
            >
              {collapsed[server] ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
              <Server size={10} className="text-violet-400/60" />
              <span className="font-medium truncate">{server}</span>
              <span className="ml-auto text-[10px] text-text-muted">{(serverTools ?? []).length}</span>
            </button>
            {!collapsed[server] && (
              <div>
                {(serverTools ?? []).map((tool) => (
                  <ToolItem key={tool.name} tool={tool} />
                ))}
              </div>
            )}
          </div>
        ))}
        {Object.keys(filtered).length === 0 && (
          <p className="px-3 py-4 text-xs text-text-muted/40 text-center italic">No tools found</p>
        )}
      </div>

      {/* Inputs section */}
      <div className="border-t border-border/30">
        <InputEditor inputs={inputs} onChange={onInputsChange} />
      </div>

      {/* Output section */}
      <div className="border-t border-border/30">
        <OutputEditor output={output} onChange={onOutputChange} stepIds={stepIds} />
      </div>
    </div>
  );
}
