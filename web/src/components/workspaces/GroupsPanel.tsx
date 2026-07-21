import { useState } from 'react';
import { Boxes, Check, Copy, CornerDownRight } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
import { annotationChips, groupEndpointURL } from '../../lib/groups';
import type { GroupsReport, GroupStatus, GroupToolStatus } from '../../lib/api';
import type { Tool } from '../../types';

interface GroupsPanelProps {
  isOpen: boolean;
  onClose: () => void;
  report: GroupsReport | null;
  // The raw tool catalog (canonical prefixed names + downstream
  // descriptions), for the original-beside-rewritten comparison.
  toolCatalog: Tool[];
}

// GroupsPanel is the Tools workspace's curation-axis surface: every group
// declared under stack.yaml `groups:`, its endpoint, and the exposed member
// surface — including exactly what a group client's model sees for renamed
// and rewritten tools, shown beside the downstream original.
export function GroupsPanel({ isOpen, onClose, report, toolCatalog }: GroupsPanelProps) {
  const groups = report?.configured ? report.groups : [];
  const [activeName, setActiveName] = useState<string | null>(null);
  const active = groups.find((g) => g.name === activeName) ?? groups[0] ?? null;

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Tool groups" size="wide">
      {groups.length === 0 ? (
        <div className="py-10 text-center space-y-2">
          <Boxes size={24} className="mx-auto text-text-muted/30" aria-hidden="true" />
          <p className="text-xs text-text-muted">No tool groups configured.</p>
          <p className="text-[11px] text-text-muted/70">
            Declare a <span className="font-mono text-text-secondary">groups:</span> block in
            stack.yaml to serve curated tool bundles at their own endpoints.
          </p>
        </div>
      ) : (
        <div className="flex gap-4 min-h-[320px] max-h-[65vh]">
          {/* Group list */}
          <div className="w-48 flex-shrink-0 border-r border-border/30 pr-3 overflow-y-auto scrollbar-dark">
            <ul aria-label="Configured groups" className="space-y-1">
              {groups.map((g) => {
                const isActive = active?.name === g.name;
                return (
                  <li key={g.name}>
                    <button
                      type="button"
                      onClick={() => setActiveName(g.name)}
                      aria-current={isActive}
                      className={cn(
                        'w-full text-left rounded-md px-2.5 py-2 border-l-2 transition-colors',
                        isActive
                          ? 'border-l-primary bg-primary/[0.07]'
                          : 'border-l-transparent hover:bg-surface-highlight/40',
                      )}
                    >
                      <div className="flex items-center gap-1.5">
                        <Boxes size={11} className={isActive ? 'text-primary' : 'text-text-muted'} aria-hidden="true" />
                        <span className="text-xs font-mono text-text-primary truncate">{g.name}</span>
                      </div>
                      <div className="text-[10px] text-text-muted mt-0.5">
                        {g.member_count} {g.member_count === 1 ? 'tool' : 'tools'}
                      </div>
                    </button>
                  </li>
                );
              })}
            </ul>
          </div>

          {/* Group detail */}
          <div className="flex-1 min-w-0 overflow-y-auto scrollbar-dark pr-1">
            {active && <GroupDetail group={active} toolCatalog={toolCatalog} />}
          </div>
        </div>
      )}
    </Modal>
  );
}

function GroupDetail({ group, toolCatalog }: { group: GroupStatus; toolCatalog: Tool[] }) {
  return (
    <div className="space-y-3">
      <div>
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-mono text-text-primary">{group.name}</span>
          <EndpointCopy endpoint={group.endpoint} />
        </div>
        {group.description && (
          <p className="text-[11px] text-text-muted mt-1">{group.description}</p>
        )}
        <p className="text-[10px] text-text-muted/70 mt-1">
          Link a client:{' '}
          <span className="font-mono text-text-secondary">
            gridctl link &lt;client&gt; --group {group.name}
          </span>
        </p>
      </div>

      <ul aria-label={`Tools exposed by ${group.name}`} className="space-y-2">
        {group.members.map((m) => (
          <MemberRow key={m.canonical} member={m} toolCatalog={toolCatalog} />
        ))}
        {group.members.length === 0 && (
          <li className="text-[11px] text-text-muted/70 italic py-3">
            No live tools resolve into this group yet. Members appear once their servers
            connect.
          </li>
        )}
      </ul>
    </div>
  );
}

function MemberRow({ member, toolCatalog }: { member: GroupToolStatus; toolCatalog: Tool[] }) {
  const chips = annotationChips(member.annotations);
  // The downstream original, for the rewritten-beside-original comparison.
  const original = member.rewritten
    ? toolCatalog.find((t) => t.name === member.canonical)?.description
    : undefined;

  return (
    <li className="rounded-lg border border-border/30 bg-background/50 px-3 py-2.5">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-xs font-mono text-text-primary">{member.name}</span>
        {member.renamed && (
          <span
            className="inline-flex items-center gap-1 text-[10px] text-text-muted"
            title="Renamed at the group's exposure boundary; enforcement uses the canonical name"
          >
            <CornerDownRight size={10} aria-hidden="true" />
            <span className="font-mono">{member.canonical}</span>
          </span>
        )}
        <span className="ml-auto flex items-center gap-1">
          {chips.map((chip) => (
            <span
              key={chip.label}
              title={chip.title}
              className={cn(
                'px-1.5 py-0.5 rounded text-[9px] font-medium uppercase tracking-wider',
                chip.tone === 'safe' && 'bg-status-running/10 text-status-running',
                chip.tone === 'danger' && 'bg-status-error/10 text-status-error',
                chip.tone === 'neutral' && 'bg-surface-highlight text-text-muted',
              )}
            >
              {chip.label}
            </span>
          ))}
        </span>
      </div>

      {member.description && (
        <p className="text-[11px] text-text-secondary mt-1.5 leading-relaxed">
          {member.rewritten && (
            <span className="text-[9px] uppercase tracking-wider text-primary/80 mr-1.5">
              rewritten
            </span>
          )}
          {member.description}
        </p>
      )}
      {original && original !== member.description && (
        <p className="text-[10px] text-text-muted/70 mt-1 leading-relaxed">
          <span className="text-[9px] uppercase tracking-wider mr-1.5">original</span>
          {original}
        </p>
      )}
    </li>
  );
}

// EndpointCopy renders the group's endpoint path with a copy-URL affordance.
function EndpointCopy({ endpoint }: { endpoint: string }) {
  const [copied, setCopied] = useState(false);
  const url = groupEndpointURL(endpoint);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard unavailable (permissions, http): leave the URL visible.
    }
  };

  return (
    <button
      type="button"
      onClick={() => void copy()}
      aria-label={`Copy endpoint URL ${url}`}
      title={url}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-md border px-2 py-0.5 text-[10px] font-mono transition-colors',
        copied
          ? 'border-status-running/40 bg-status-running/10 text-status-running'
          : 'border-border/40 bg-background/40 text-text-muted hover:text-text-secondary hover:border-border',
      )}
    >
      {copied ? <Check size={10} aria-hidden="true" /> : <Copy size={10} aria-hidden="true" />}
      {endpoint}
    </button>
  );
}
