import { useRef, useState, type ReactNode } from 'react';
import { ArrowDown, ArrowUp, CircleHelp } from 'lucide-react';
import { cn } from '../../lib/cn';
import { NODE_STATUS_LABELS } from '../../lib/nodeStatus';
import { GLYPH_META, type CompactGlyphKind } from '../../lib/graph/serverState';
import type { NodeStatus } from '../../types';
import { Popover } from '../ui/Popover';
import { StatusDot } from '../ui/StatusDot';
import { COMPACT_GLYPH_COLORS, COMPACT_GLYPH_ICONS } from './compactGlyphs';

const STATUSES = Object.keys(NODE_STATUS_LABELS) as NodeStatus[];
const GLYPHS = Object.keys(GLYPH_META) as CompactGlyphKind[];

function Heading({ children }: { children: string }) {
  return (
    <div className="px-2 pt-2 pb-1 text-3xs uppercase tracking-[0.18em] text-text-muted">
      {children}
    </div>
  );
}

function Row({ mark, label, description }: { mark: ReactNode; label: string; description?: string }) {
  return (
    <li className="flex items-start gap-2 px-2 py-1 text-2xs">
      <span className="flex-shrink-0 w-8 h-4 inline-flex items-center justify-center">{mark}</span>
      <span className="min-w-0">
        <span className="text-text-primary">{label}</span>
        {description && <span className="block text-text-muted">{description}</span>}
      </span>
    </li>
  );
}

/**
 * Explains the Stack canvas card marks: status dot shapes, compact card
 * glyphs, and header counters. Built from the same maps the cards use so the
 * two cannot drift apart. Opens upward from the bottom-left control panel.
 */
export function CanvasLegend() {
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={cn('control-button', open && 'ring-1 ring-primary/30')}
        title="Legend"
        aria-label="Legend"
        aria-haspopup="dialog"
        aria-expanded={open}
      >
        <CircleHelp className="w-4 h-4" />
      </button>

      <Popover
        open={open}
        onClose={() => setOpen(false)}
        triggerRef={triggerRef}
        role="dialog"
        aria-label="Canvas legend"
        className="absolute bottom-full left-0 mb-2 w-72 max-h-[70vh] overflow-y-auto p-1"
      >
        <Heading>Status</Heading>
        <ul>
          {STATUSES.map((status) => (
            <Row
              key={status}
              mark={<StatusDot status={status} size="md" pulse={false} />}
              label={NODE_STATUS_LABELS[status]}
            />
          ))}
        </ul>

        <Heading>Compact card marks</Heading>
        <ul>
          {GLYPHS.map((kind) => {
            const Icon = COMPACT_GLYPH_ICONS[kind];
            return (
              <Row
                key={kind}
                mark={<Icon size={12} className={COMPACT_GLYPH_COLORS[kind]} aria-hidden="true" />}
                label={GLYPH_META[kind].label}
                description={GLYPH_META[kind].description}
              />
            );
          })}
        </ul>

        <Heading>Counters</Heading>
        <ul>
          <Row
            mark={<span className="font-mono text-3xs text-text-muted">Nt</span>}
            label="Tools exposed"
          />
          <Row
            mark={<span className="font-mono text-3xs text-tertiary-light/80">×N</span>}
            label="Replicas"
          />
          <Row
            mark={<span className="font-mono text-3xs text-tertiary-light/80">×c/t</span>}
            label="Autoscale replicas, current of target"
          />
          <Row
            mark={
              <span className="inline-flex text-tertiary-light/80" aria-hidden="true">
                <ArrowUp size={10} />
                <ArrowDown size={10} />
              </span>
            }
            label="Scaling up or down"
            description="A pulsing ring marks a scale-up; a still ring marks a scale-down"
          />
        </ul>
      </Popover>
    </div>
  );
}
