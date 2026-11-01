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
  return 'bg-tertiary/10 text-tertiary-light';
}

/**
 * Tailwind CSS classes for external MCP server styling.
 * Use these to maintain consistent styling across components.
 */
export const EXTERNAL_STYLES = {
  // Background gradient for cards
  bgGradient: 'from-surface/95 to-tertiary/[0.03]',
  // Border color
  border: 'border-tertiary/30',
  // Selected state border
  borderSelected: 'border-tertiary',
  // Glow effect for selected state
  shadow: 'shadow-[0_0_15px_--alpha(var(--color-tertiary)/30%)]',
  // Ring for selected state
  ring: 'ring-1 ring-tertiary/30',
  // Header background
  headerBg: 'border-tertiary/10 bg-tertiary/[0.03]',
  // Accent line gradient
  accentLine: 'bg-gradient-to-r from-transparent via-tertiary/40 to-transparent',
  // Accent line vertical (for sidebar)
  accentLineVertical: 'bg-gradient-to-b from-tertiary/40 via-tertiary/20 to-transparent',
  // Icon container
  iconContainer: 'bg-tertiary/10 border-tertiary/20',
  // Icon color
  iconColor: 'text-tertiary-light',
  // Badge styling
  badge: 'bg-tertiary/20 border border-tertiary/40 text-tertiary-light',
} as const;

