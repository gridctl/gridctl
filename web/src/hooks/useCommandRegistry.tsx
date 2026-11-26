import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
} from 'react';
import type { ReactNode } from 'react';
import type { PaletteCommand, PaletteSection } from '../types/palette';
import type { Workspace } from '../types/workspace';
import { readPref, writePref } from '../lib/preferences';
import { parseFrecency, type FrecencyEntry, type FrecencyMap } from '../lib/paletteFrecency';
import { normalizePaletteQuery, scorePaletteCommand } from '../lib/paletteScore';

const FRECENCY_NAME = 'palette-frecency';

function loadFrecency(): FrecencyMap {
  return parseFrecency(readPref(FRECENCY_NAME));
}

function saveFrecency(map: FrecencyMap): void {
  writePref(FRECENCY_NAME, JSON.stringify(Object.fromEntries(map)));
}

// Score decays to 0 over 7 days; multiplied by use count
function computeFrecencyScore(entry: FrecencyEntry | undefined): number {
  if (!entry) return 0;
  const ageMs = Date.now() - entry.lastUsed;
  const recency = Math.max(0, 1 - ageMs / (7 * 24 * 60 * 60 * 1000));
  return entry.count * recency;
}

interface CommandRegistryContextValue {
  commands: PaletteCommand[];
  registerCommands: (sectionId: string, cmds: PaletteCommand[]) => void;
  unregisterCommands: (sectionId: string) => void;
  recordUsage: (commandId: string) => void;
  getSortedCommands: (
    query?: string,
    scopeSection?: PaletteSection,
    activeWorkspace?: Workspace,
  ) => PaletteCommand[];
  getRecentCommands: (limit?: number, activeWorkspace?: Workspace) => PaletteCommand[];
}

const CommandRegistryContext = createContext<CommandRegistryContextValue | null>(null);

export function CommandRegistryProvider({ children }: { children: ReactNode }) {
  const [sectionMap, setSectionMap] = useState<Map<string, PaletteCommand[]>>(new Map());
  const frecencyRef = useRef<FrecencyMap>(loadFrecency());

  const registerCommands = useCallback((sectionId: string, cmds: PaletteCommand[]) => {
    setSectionMap((prev) => {
      const next = new Map(prev);
      next.set(sectionId, cmds);
      return next;
    });
  }, []);

  const unregisterCommands = useCallback((sectionId: string) => {
    setSectionMap((prev) => {
      const next = new Map(prev);
      next.delete(sectionId);
      return next;
    });
  }, []);

  const commands = useMemo(() => {
    const all: PaletteCommand[] = [];
    for (const cmds of sectionMap.values()) {
      all.push(...cmds);
    }
    return all;
  }, [sectionMap]);

  const recordUsage = useCallback((commandId: string) => {
    const existing = frecencyRef.current.get(commandId);
    frecencyRef.current.set(commandId, {
      count: (existing?.count ?? 0) + 1,
      lastUsed: Date.now(),
    });
    saveFrecency(frecencyRef.current);
  }, []);

  const getSortedCommands = useCallback(
    (
      query?: string,
      scopeSection?: PaletteSection,
      activeWorkspace?: Workspace,
    ): PaletteCommand[] => {
      let filtered = commands;

      if (activeWorkspace) {
        filtered = filtered.filter(
          (c) => !c.workspaces || c.workspaces.includes(activeWorkspace),
        );
      }

      if (scopeSection) {
        filtered = filtered.filter((c) => c.section === scopeSection);
      }

      const scores = new Map<string, number>();
      for (const c of filtered) {
        scores.set(c.id, computeFrecencyScore(frecencyRef.current.get(c.id)));
      }
      const byFrecency = (a: PaletteCommand, b: PaletteCommand) =>
        (scores.get(b.id) ?? 0) - (scores.get(a.id) ?? 0);

      const q = query ? normalizePaletteQuery(query) : '';
      if (!q) return [...filtered].sort(byFrecency);

      // Match quality first; frecency only orders commands within a tier.
      // Array.prototype.sort is stable, so ties keep registration order.
      const ranked: { command: PaletteCommand; tier: number }[] = [];
      for (const command of filtered) {
        const tier = scorePaletteCommand(command, q);
        if (tier !== null) ranked.push({ command, tier });
      }
      ranked.sort((a, b) => a.tier - b.tier || byFrecency(a.command, b.command));
      return ranked.map((r) => r.command);
    },
    [commands]
  );

  const getRecentCommands = useCallback(
    (limit = 5, activeWorkspace?: Workspace): PaletteCommand[] => {
      return commands
        .filter((c) => frecencyRef.current.has(c.id))
        .filter((c) => !activeWorkspace || !c.workspaces || c.workspaces.includes(activeWorkspace))
        .sort(
          (a, b) =>
            (frecencyRef.current.get(b.id)?.lastUsed ?? 0) -
            (frecencyRef.current.get(a.id)?.lastUsed ?? 0)
        )
        .slice(0, limit);
    },
    [commands]
  );

  const value = useMemo(
    () => ({
      commands,
      registerCommands,
      unregisterCommands,
      recordUsage,
      getSortedCommands,
      getRecentCommands,
    }),
    [commands, registerCommands, unregisterCommands, recordUsage, getSortedCommands, getRecentCommands]
  );

  return (
    <CommandRegistryContext.Provider value={value}>
      {children}
    </CommandRegistryContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components -- context provider + hook pair; splitting the file would separate the API from its provider
export function useCommandRegistry(): CommandRegistryContextValue {
  const ctx = useContext(CommandRegistryContext);
  if (!ctx) throw new Error('useCommandRegistry must be used within a CommandRegistryProvider');
  return ctx;
}
