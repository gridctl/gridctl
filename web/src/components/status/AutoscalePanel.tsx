import { useMemo, useState } from 'react';
import { ChevronDown, ChevronRight, TrendingUp, TrendingDown, Minus } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { AutoscaleStatus } from '../../types';
import type { AutoscaleSample, AutoscaleDecision } from '../../stores/useStackStore';

interface AutoscalePanelProps {
  status: AutoscaleStatus;
  history: AutoscaleSample[];
  decisions: AutoscaleDecision[];
}

export function AutoscalePanel({ status, history, decisions }: AutoscalePanelProps) {
  return (
    <div className="space-y-3">
      <Headline status={status} />
      <DwellPhrase status={status} />
      <Sparkline history={history} max={status.max} />
      <DecisionFeed decisions={decisions} />
    </div>
  );
}

function Headline({ status }: { status: AutoscaleStatus }) {
  return (
    <div className="flex items-baseline gap-2 text-sm text-text-primary font-mono tracking-tight">
      <span className="text-text-muted">Current</span>
      <span className="text-violet-300 font-semibold">{status.current}</span>
      <span className="text-text-muted">/ Target</span>
      <span className="text-violet-300 font-semibold">{status.target}</span>
      <span className="text-text-muted">· Range</span>
      <span className="text-text-secondary">
        {status.min}–{status.max}
      </span>
    </div>
  );
}

function DwellPhrase({ status }: { status: AutoscaleStatus }) {
  const phrase = dwellPhrase(status);
  return (
    <p className="text-xs text-text-secondary leading-relaxed" data-testid="autoscale-dwell">
      {phrase}
    </p>
  );
}

export function dwellPhrase(status: AutoscaleStatus): string {
  if (status.idleToZero && status.current === 0) {
    return 'Idle · scaled to zero';
  }
  if (status.lastDecision === 'up') {
    return `Scaling up · median in-flight ${status.medianInFlight}, target ${status.targetInFlight}`;
  }
  if (status.lastDecision === 'down') {
    return `Scaling down · median in-flight ${status.medianInFlight} below target ${status.targetInFlight}`;
  }
  // noop
  if (status.current === status.target) {
    return `Stable · median in-flight ${status.medianInFlight}, target ${status.targetInFlight}`;
  }
  return `Holding · current ${status.current}, target ${status.target}`;
}

interface SparklineProps {
  history: AutoscaleSample[];
  max: number;
}

// Inline SVG sparkline — no chart library. Width fills container via
// viewBox; container sets fixed ~40px height. Memoized so the only work
// per render is bookkeeping when history/max are unchanged.
function Sparkline({ history, max }: SparklineProps) {
  const W = 200;
  const H = 40;

  const geom = useMemo(() => {
    if (history.length === 0) return null;
    const maxSeen = Math.max(
      max,
      ...history.map((s) => s.current),
      ...history.map((s) => s.target),
    );
    const yMax = Math.max(1, maxSeen * 1.1);
    const n = history.length;
    const xFor = (i: number) => (n <= 1 ? 0 : (i / (n - 1)) * W);
    const yFor = (v: number) => H - (v / yMax) * H;
    const toPath = (values: number[]) =>
      values.map((v, i) => `${i === 0 ? 'M' : 'L'} ${xFor(i).toFixed(2)} ${yFor(v).toFixed(2)}`).join(' ');
    const observedMin = Math.min(...history.map((s) => s.current));
    return {
      currentPath: toPath(history.map((s) => s.current)),
      targetPath: toPath(history.map((s) => s.target)),
      points: history.map((s, i) => ({ cx: xFor(i), cy: yFor(s.current), sample: s })),
      minY: yFor(observedMin),
      maxY: yFor(maxSeen),
    };
  }, [history, max]);

  if (!geom) {
    return (
      <div className="h-10 rounded-md bg-background/40 border border-border/30 flex items-center justify-center">
        <span className="text-[10px] text-text-muted">Collecting samples…</span>
      </div>
    );
  }

  return (
    <svg
      data-testid="autoscale-sparkline"
      viewBox={`0 0 ${W} ${H}`}
      preserveAspectRatio="none"
      className="w-full h-10 block"
      role="img"
      aria-label="Autoscale current vs target over time"
    >
      {/* Min/max guide bands */}
      <line x1={0} x2={W} y1={geom.minY} y2={geom.minY} stroke="rgb(139 92 246)" strokeOpacity={0.15} strokeWidth={1} />
      <line x1={0} x2={W} y1={geom.maxY} y2={geom.maxY} stroke="rgb(139 92 246)" strokeOpacity={0.15} strokeWidth={1} />
      {/* Target (dashed, 50% opacity) */}
      <path
        d={geom.targetPath}
        fill="none"
        stroke="rgb(139 92 246)"
        strokeOpacity={0.5}
        strokeWidth={1.5}
        strokeDasharray="3 3"
        vectorEffect="non-scaling-stroke"
      />
      {/* Current (solid 1.5px violet) */}
      <path
        data-testid="autoscale-sparkline-current"
        d={geom.currentPath}
        fill="none"
        stroke="rgb(139 92 246)"
        strokeWidth={1.5}
        vectorEffect="non-scaling-stroke"
      />
      {/* Minimal tooltip points: <title> on each current-sample anchor */}
      {geom.points.map((p) => (
        <circle key={p.sample.t} cx={p.cx} cy={p.cy} r={1.5} fill="rgb(139 92 246)">
          <title>{`current ${p.sample.current} · target ${p.sample.target} · median in-flight ${p.sample.medianInFlight}`}</title>
        </circle>
      ))}
    </svg>
  );
}

function DecisionFeed({ decisions }: { decisions: AutoscaleDecision[] }) {
  const [open, setOpen] = useState(false);
  const count = decisions.length;

  return (
    <div className="border border-border/30 rounded-md bg-background/30">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="w-full flex items-center justify-between px-2.5 py-1.5 hover:bg-surface-highlight/40 transition-colors rounded-md"
        aria-expanded={open}
      >
        <div className="flex items-center gap-1.5">
          <span className="text-[10px] uppercase tracking-widest text-text-muted font-medium">
            Recent Decisions
          </span>
          <span className="text-[10px] text-text-muted bg-surface-elevated px-1.5 py-0.5 rounded-md font-mono">
            {count}
          </span>
        </div>
        {open ? (
          <ChevronDown size={12} className="text-text-muted" />
        ) : (
          <ChevronRight size={12} className="text-text-muted" />
        )}
      </button>
      {open && (
        <ul className="px-2.5 py-2 space-y-1" data-testid="autoscale-decision-feed">
          {count === 0 && (
            <li className="text-[11px] text-text-muted italic">No scale events yet</li>
          )}
          {decisions.map((d) => (
            <DecisionRow key={`${d.t}-${d.kind}`} decision={d} />
          ))}
        </ul>
      )}
    </div>
  );
}

function DecisionRow({ decision }: { decision: AutoscaleDecision }) {
  const Icon = decision.kind === 'up' ? TrendingUp : decision.kind === 'down' ? TrendingDown : Minus;
  const color =
    decision.kind === 'up' ? 'text-primary' : decision.kind === 'down' ? 'text-secondary' : 'text-text-muted';
  return (
    <li className="flex items-center gap-2 text-[11px] font-mono">
      <span className="text-text-muted">{formatTime(decision.t)}</span>
      <span className={cn('inline-flex items-center gap-1 px-1.5 py-0.5 rounded-md', color)}>
        <Icon size={10} />
        <span className="uppercase tracking-wider">{decision.kind}</span>
      </span>
      <span className="text-text-secondary truncate">
        {decision.from}→{decision.to}
      </span>
    </li>
  );
}

function formatTime(t: number): string {
  const d = new Date(t);
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
