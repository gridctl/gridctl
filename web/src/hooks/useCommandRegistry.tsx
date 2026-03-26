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

const FRECENCY_KEY = 'gridctl-palette-frecency';

interface FrecencyEntry {
  count: number;
  lastUsed: number;
}

type FrecencyMap = Map<string, FrecencyEntry>;

function loadFrecency(): FrecencyMap {
  try {
    const raw = localStorage.getItem(FRECENCY_KEY);
    if (!raw) return new Map();
    const parsed = JSON.parse(raw) as Record<string, FrecencyEntry>;
    return new Map(Object.entries(parsed));
  } catch {
    return new Map();
  }
}

function saveFrecency(map: FrecencyMap): void {
  try {
    localStorage.setItem(FRECENCY_KEY, JSON.stringify(Object.fromEntries(map)));
  } catch {
    // Ignore write errors (e.g. private browsing quota)
  }
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
  getSortedCommands: (query?: string, scopeSection?: PaletteSection) => PaletteCommand[];
  getRecentCommands: (limit?: number) => PaletteCommand[];
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
    (query?: string, scopeSection?: PaletteSection): PaletteCommand[] => {
      let filtered = commands;

      if (scopeSection) {
        filtered = filtered.filter((c) => c.section === scopeSection);
      }

      if (query?.trim()) {
        const q = query.toLowerCase();
        filtered = filtered.filter((c) => {
          const haystack = [c.label, ...(c.keywords ?? [])].join(' ').toLowerCase();
          // Fuzzy: every character of q appears in order in haystack
          let pos = 0;
          for (const ch of q) {
            const found = haystack.indexOf(ch, pos);
            if (found === -1) return false;
            pos = found + 1;
          }
          return true;
        });
      }

      return [...filtered].sort(
        (a, b) =>
          computeFrecencyScore(frecencyRef.current.get(b.id)) -
          computeFrecencyScore(frecencyRef.current.get(a.id))
      );
    },
    [commands]
  );

  const getRecentCommands = useCallback(
    (limit = 5): PaletteCommand[] => {
      return commands
        .filter((c) => frecencyRef.current.has(c.id))
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

export function useCommandRegistry(): CommandRegistryContextValue {
  const ctx = useContext(CommandRegistryContext);
  if (!ctx) throw new Error('useCommandRegistry must be used within a CommandRegistryProvider');
  return ctx;
}
