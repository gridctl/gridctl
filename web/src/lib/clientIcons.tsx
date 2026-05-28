import type { ComponentType } from 'react';
import { Monitor } from 'lucide-react';
// Named imports only — @lobehub/icons sets `sideEffects: false`, so Vite
// tree-shakes these down to the individual icon modules we reference.
import {
  Claude,
  Cursor,
  Windsurf,
  Gemini,
  OpenCode,
  Grok,
  Cline,
  RooCode,
  Goose,
  Codex,
} from '@lobehub/icons';

/**
 * Common surface shared by the lobehub mono icons and the lucide fallback.
 * Both accept a numeric `size` and a `className` (color is driven via
 * `currentColor`, so the parent's text color decides the glyph color).
 */
export interface ClientIconProps {
  size?: number;
  className?: string;
}

/**
 * Maps a client `slug` (see pkg/provisioner/provisioner.go) to its brand icon,
 * rendered in lobehub's monochrome variant. Claude Desktop and Claude Code
 * intentionally share the Claude mark; their node labels disambiguate them.
 *
 * Slugs without an entry fall back to the generic Monitor glyph (see
 * getClientIcon). lobehub 5.x does not ship marks for vscode, continue,
 * anythingllm, or zed, so those currently render the fallback.
 *
 * `codex` is pre-wired for a future OpenAI/Codex client; no such provisioner
 * slug is emitted today, so it stays dormant.
 */
const CLIENT_ICONS: Record<string, ComponentType<ClientIconProps>> = {
  claude: Claude,
  'claude-code': Claude,
  cursor: Cursor,
  windsurf: Windsurf,
  gemini: Gemini,
  opencode: OpenCode,
  grok: Grok,
  cline: Cline,
  roo: RooCode,
  goose: Goose,
  codex: Codex,
};

/**
 * Returns the brand icon component for a client slug, or the Monitor fallback
 * when the slug is unmapped. Always returns a renderable component, so a node
 * never appears blank.
 */
export function getClientIcon(slug: string): ComponentType<ClientIconProps> {
  return CLIENT_ICONS[slug] ?? Monitor;
}
