import { type AgentNode, type SkillGraph, editorURL } from '../../../lib/agent-api';
import { styleFor } from './kind-style';
import type { NodeTrace } from './useRunTrace';
import { TracePill } from './TracePill';
import { cn } from '../../../lib/cn';

interface NodeListProps {
  graph: SkillGraph;
  skillDir: string;
  selected: string | null;
  onSelect: (nodeID: string | null) => void;
  trace: Record<string, NodeTrace>;
}

/**
 * NodeList is the Slice 1 surface — a flat textual list of every
 * recognised primitive call site. Each row is one node; clicking
 * the file:line opens the source in $EDITOR. Trace overlay (Slice
 * 2) decorates the right edge with status pills + latency.
 *
 * The list view is deliberately information-dense: developers
 * scanning a typed graph want to see kind, label, file:line, and
 * trace status without a click. This is the boring useful thing
 * that ships first.
 */
export function NodeList({ graph, skillDir, selected, onSelect, trace }: NodeListProps) {
  if (graph.nodes.length === 0 && !graph.parse_error) {
    return (
      <EmptyGraph
        title="No primitives yet"
        body="Add a tool(), llm(), parallel(), handoff(), or approval() call to render a node."
      />
    );
  }
  return (
    <div className="font-mono text-sm">
      {graph.parse_error && (
        <div className="mx-6 mt-6 px-4 py-3 rounded-md border border-status-error/30 bg-status-error/5 text-status-error">
          <div className="text-xs uppercase tracking-[0.2em] text-status-error/70 mb-1">
            stale graph — last good
          </div>
          <pre className="whitespace-pre-wrap text-xs">{graph.parse_error}</pre>
        </div>
      )}
      <ol className="px-6 py-6 space-y-px">
        {graph.nodes.map((node, i) => (
          <NodeRow
            key={node.id}
            node={node}
            index={i}
            isLast={i === graph.nodes.length - 1}
            skillDir={skillDir}
            selected={selected === node.id}
            onSelect={() => onSelect(selected === node.id ? null : node.id)}
            trace={trace[node.id]}
          />
        ))}
      </ol>
    </div>
  );
}

interface NodeRowProps {
  node: AgentNode;
  index: number;
  isLast: boolean;
  skillDir: string;
  selected: boolean;
  onSelect: () => void;
  trace: NodeTrace | undefined;
}

function NodeRow({ node, index, isLast, skillDir, selected, onSelect, trace }: NodeRowProps) {
  const style = styleFor(node.kind);
  const indent = `${String(index + 1).padStart(2, '0')}`;
  return (
    <li
      onClick={onSelect}
      className={cn(
        'group grid grid-cols-[44px_88px_1fr_auto_auto] items-center gap-4 px-3 py-2.5 rounded-md',
        'border border-transparent transition-colors duration-100',
        'cursor-pointer',
        selected
          ? 'bg-surface-elevated/80 border-border'
          : 'hover:bg-surface/40 hover:border-border-subtle',
      )}
    >
      <span className="text-text-muted text-xs tabular-nums select-none">{indent}</span>
      <span
        className={cn(
          'inline-flex items-center justify-center gap-1.5 px-2 py-0.5 rounded text-[10px]',
          'uppercase tracking-[0.16em] font-medium',
          style.badgeBg,
          style.badgeText,
          'border',
          style.border,
        )}
      >
        <span className="text-sm leading-none">{style.glyph}</span>
        {style.label}
      </span>
      <span className="truncate text-text-primary">{node.label}</span>
      <a
        href={editorURL(skillDir, node.file, node.line)}
        onClick={(e) => e.stopPropagation()}
        className={cn(
          'opacity-0 group-hover:opacity-100 focus:opacity-100',
          'text-xs text-text-muted hover:text-text-primary transition-colors',
          'tabular-nums whitespace-nowrap',
        )}
        title={`${node.file}:${node.line}`}
      >
        {shortenPath(node.file)}:{node.line}
      </a>
      <TracePill trace={trace} />
      {/* Visual connector — every row except the last gets a thin descender */}
      {!isLast && (
        <span aria-hidden className="col-start-2 row-start-2 h-3 -mt-1 border-l border-border-subtle ml-3" />
      )}
    </li>
  );
}

function shortenPath(file: string): string {
  if (file.length <= 36) return file;
  return '…' + file.slice(file.length - 35);
}

function EmptyGraph({ title, body }: { title: string; body: string }) {
  return (
    <div className="flex flex-col items-center justify-center text-center px-12 py-24">
      <div className="font-sans text-text-muted/60 text-xs uppercase tracking-[0.4em] mb-3">
        empty graph
      </div>
      <h2 className="font-sans text-2xl text-text-secondary mb-2">{title}</h2>
      <p className="text-text-muted text-sm max-w-sm leading-relaxed">{body}</p>
    </div>
  );
}
