import { Wifi, Server, Radio, type LucideIcon } from 'lucide-react';
import type { Transport } from '../types';

/**
 * Get the appropriate icon component for a transport type.
 * - HTTP: Wifi icon (network communication)
 * - Stdio: Server icon (local container)
 * - SSE: Radio icon (streaming)
 */
export function getTransportIcon(transport: Transport | null): LucideIcon {
  switch (transport) {
    case 'stdio':
      return Server;
    case 'sse':
      return Radio;
    case 'http':
    default:
      return Wifi;
  }
}

/**
 * Get the Tailwind CSS classes for transport badge styling.
 * Returns an object with background and text color classes.
 */
export function getTransportColorClasses(_transport: Transport | null): string {
  // All MCP servers use violet theme for transport badges
  return 'bg-violet-500/10 text-violet-400';
}

/**
 * Tailwind CSS classes for external MCP server styling.
 * Use these to maintain consistent styling across components.
 */
export const EXTERNAL_STYLES = {
  // Background gradient for cards
  bgGradient: 'from-surface/95 to-violet-500/[0.03]',
  // Border color
  border: 'border-violet-500/30',
  // Selected state border
  borderSelected: 'border-violet-500',
  // Glow effect for selected state
  shadow: 'shadow-[0_0_15px_rgba(139,92,246,0.3)]',
  // Ring for selected state
  ring: 'ring-1 ring-violet-500/30',
  // Header background
  headerBg: 'border-violet-500/10 bg-violet-500/[0.03]',
  // Accent line gradient
  accentLine: 'bg-gradient-to-r from-transparent via-violet-500/40 to-transparent',
  // Accent line vertical (for sidebar)
  accentLineVertical: 'bg-gradient-to-b from-violet-500/40 via-violet-500/20 to-transparent',
  // Icon container
  iconContainer: 'bg-violet-500/10 border-violet-500/20',
  // Icon color
  iconColor: 'text-violet-400',
  // Badge styling
  badge: 'bg-violet-500/20 border border-violet-500/40 text-violet-400',
} as const;

