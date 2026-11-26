import { Ban, Hourglass, KeyRound, Lock, LockOpen, Moon, RotateCw, Square, TriangleAlert, type LucideIcon } from 'lucide-react';
import type { CompactGlyphKind } from '../../lib/graph/serverState';

/** Icon per compact card glyph, shared by the card and the canvas legend. */
export const COMPACT_GLYPH_ICONS: Record<CompactGlyphKind, LucideIcon> = {
  disabled: Ban,
  'needs-auth': KeyRound,
  error: TriangleAlert,
  restarting: RotateCw,
  initializing: Hourglass,
  stopped: Square,
  idle: Moon,
  drift: LockOpen,
  blocked: Lock,
};

/** Color per compact card glyph, matching the full-card strips. */
export const COMPACT_GLYPH_COLORS: Record<CompactGlyphKind, string> = {
  disabled: 'text-text-muted',
  'needs-auth': 'text-status-pending',
  error: 'text-status-error',
  restarting: 'text-status-pending',
  initializing: 'text-status-pending',
  stopped: 'text-text-muted',
  idle: 'text-text-muted',
  drift: 'text-status-pending',
  blocked: 'text-status-error',
};
