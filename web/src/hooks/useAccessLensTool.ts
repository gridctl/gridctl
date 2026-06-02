import { useCallback } from 'react';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';

const EMPTY: string[] = [];

// useAccessLensTool exposes the per-tool edit affordance the canvas fan-out
// pills use in Access Lens. It is the canvas counterpart to the slide-over's
// ServerToolScopeGroup: same draft store, expressed as direct manipulation.
//
// editMode is true only while the lens targets the selected client AND this
// server is granted in the draft (you cannot scope tools of a server the client
// can't reach). When false, the pills stay inspect-only.
//
// toggle() encodes the "all minus one" semantic: clicking a single pill on an
// unrestricted ("All") server switches it to Custom with every OTHER tool
// selected, so one click removes exactly that tool. On an already-custom server
// it is a plain per-tool toggle.
export function useAccessLensTool(serverName: string) {
  const enabled = useAccessLensStore((s) => s.enabled);
  const clientSlug = useAccessLensStore((s) => s.clientSlug);
  const selectedNodeId = useStackStore((s) => s.selectedNodeId);
  const granted = useAccessLensStore((s) => s.draft.includes(serverName));
  const mode = useAccessLensStore((s) => s.toolMode[serverName] ?? 'all');
  const customSel = useAccessLensStore((s) => s.customSel[serverName]) ?? EMPTY;
  const setServerToolMode = useAccessLensStore((s) => s.setServerToolMode);
  const toggleToolAction = useAccessLensStore((s) => s.toggleTool);
  const setCustomTools = useAccessLensStore((s) => s.setCustomTools);
  const serverTools = useStackStore(
    (s) => s.mcpServers.find((m) => m.name === serverName)?.tools ?? EMPTY,
  );

  const editMode =
    enabled && clientSlug != null && selectedNodeId === `client-${clientSlug}` && granted;

  const isOn = useCallback(
    (tool: string) => mode === 'all' || customSel.includes(tool),
    [mode, customSel],
  );

  const toggle = useCallback(
    (tool: string) => {
      if (mode === 'all') {
        setServerToolMode(serverName, 'custom');
        setCustomTools(
          serverName,
          serverTools.filter((t) => t !== tool),
        );
      } else {
        toggleToolAction(serverName, tool);
      }
    },
    [mode, serverName, serverTools, setServerToolMode, setCustomTools, toggleToolAction],
  );

  return { editMode, isOn, toggle };
}
