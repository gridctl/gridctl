import type { NodeKind } from '../../../lib/agent-api';

/**
 * Per-kind visual treatment shared between NodeList (Slice 1/2) and
 * Canvas (Slice 3). Keeping the colour map in one place keeps the
 * two views legibly cross-referencing — a tool node looks the same
 * whether you're scanning the list or the graph.
 *
 * Colours hook into the existing Obsidian Observatory palette
 * (--color-primary, --color-secondary, --color-tertiary, status
 * tokens) so the IDE feels like part of gridctl, not an island.
 */
export interface KindStyle {
  label: string;
  glyph: string;
  // Tailwind class fragments
  badgeBg: string;
  badgeText: string;
  border: string;
  ring: string;
}

const styles: Record<NodeKind, KindStyle> = {
  tool: {
    label: 'tool',
    glyph: '·',
    badgeBg: 'bg-secondary/15',
    badgeText: 'text-secondary-light',
    border: 'border-secondary/35',
    ring: 'shadow-[inset_0_0_0_1px_rgba(20,184,166,0.25)]',
  },
  llm: {
    label: 'llm',
    glyph: '◇',
    badgeBg: 'bg-primary/15',
    badgeText: 'text-primary-light',
    border: 'border-primary/35',
    ring: 'shadow-[inset_0_0_0_1px_rgba(245,158,11,0.25)]',
  },
  parallel: {
    label: 'parallel',
    glyph: '∥',
    badgeBg: 'bg-tertiary/15',
    badgeText: 'text-tertiary-light',
    border: 'border-tertiary/35',
    ring: 'shadow-[inset_0_0_0_1px_rgba(167,139,250,0.25)]',
  },
  handoff: {
    label: 'handoff',
    glyph: '→',
    badgeBg: 'bg-status-running/15',
    badgeText: 'text-status-running',
    border: 'border-status-running/35',
    ring: 'shadow-[inset_0_0_0_1px_rgba(16,185,129,0.25)]',
  },
  approval: {
    label: 'approval',
    glyph: '✦',
    badgeBg: 'bg-status-pending/15',
    badgeText: 'text-status-pending',
    border: 'border-status-pending/35',
    ring: 'shadow-[inset_0_0_0_1px_rgba(234,179,8,0.25)]',
  },
};

export function styleFor(kind: NodeKind): KindStyle {
  return styles[kind] ?? {
    label: String(kind),
    glyph: '?',
    badgeBg: 'bg-surface',
    badgeText: 'text-text-muted',
    border: 'border-border',
    ring: '',
  };
}
