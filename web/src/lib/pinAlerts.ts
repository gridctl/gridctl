import type { ContextPin, ServerPins, SkillPin } from './api';
import { contextHasAlertFindings, serverHasAlertFindings, skillHasAlertFindings } from '../stores/usePinsStore';
import { skillPinsUrl } from './skillGovernance';
import { contextPinsUrl } from './contextGovernance';

export type PinAlertKind = 'server' | 'skill' | 'fragment';
export type PinAlertView = 'drift' | 'findings';

/** Names affected by drift and by warn-or-critical findings for one pin kind. */
export interface AffectedNames {
  drift: Set<string>;
  findings: Set<string>;
}

/** Newly affected names per kind, each sorted with localeCompare. */
export interface NewNames {
  server: string[];
  skill: string[];
  fragment: string[];
}

export interface PinAlert {
  message: string;
  target: string;
}

function collect<T>(
  record: Record<string, T>,
  isDrift: (pin: T) => boolean,
  hasFindings: (pin: T) => boolean,
): AffectedNames {
  const drift = new Set<string>();
  const findings = new Set<string>();
  for (const [name, pin] of Object.entries(record)) {
    if (isDrift(pin)) drift.add(name);
    if (hasFindings(pin)) findings.add(name);
  }
  return { drift, findings };
}

export function affectedServers(pins: Record<string, ServerPins>): AffectedNames {
  return collect(pins, (sp) => sp.status === 'drift', serverHasAlertFindings);
}

export function affectedSkills(pins: Record<string, SkillPin>): AffectedNames {
  return collect(pins, (p) => p.status === 'drift', skillHasAlertFindings);
}

export function affectedContexts(pins: Record<string, ContextPin>): AffectedNames {
  return collect(pins, (p) => p.status === 'drift', contextHasAlertFindings);
}

/** Names in next that are absent from prev, sorted. */
export function newlyAffected(prev: Set<string>, next: Set<string>): string[] {
  return [...next].filter((name) => !prev.has(name)).sort((a, b) => a.localeCompare(b));
}

const PLURALS: Record<PinAlertKind, string> = {
  server: 'servers',
  skill: 'skills',
  fragment: 'fragments',
};

function joinParts(parts: string[]): string {
  if (parts.length <= 1) return parts.join('');
  if (parts.length === 2) return `${parts[0]} and ${parts[1]}`;
  return `${parts.slice(0, -1).join(', ')}, and ${parts[parts.length - 1]}`;
}

function subject(names: NewNames): string | null {
  const kinds: PinAlertKind[] = ['server', 'skill', 'fragment'];
  const total = kinds.reduce((acc, kind) => acc + names[kind].length, 0);
  if (total === 0) return null;
  if (total === 1) {
    const kind = kinds.find((k) => names[k].length === 1) as PinAlertKind;
    return `${kind} ${names[kind][0]}`;
  }
  const parts = kinds
    .filter((kind) => names[kind].length > 0)
    .map((kind) => {
      const count = names[kind].length;
      return `${count} ${count === 1 ? kind : PLURALS[kind]}`;
    });
  return joinParts(parts);
}

function targetFor(names: NewNames, view: PinAlertView): string {
  if (names.server.length > 0) {
    return `/pins?server=${encodeURIComponent(names.server[0])}&view=${view}`;
  }
  if (names.skill.length > 0) return skillPinsUrl(names.skill[0], view);
  return contextPinsUrl(names.fragment[0], view);
}

export function driftAlert(names: NewNames): PinAlert | null {
  const s = subject(names);
  if (!s) return null;
  return { message: `Pin drift detected on ${s}`, target: targetFor(names, 'drift') };
}

export function findingsAlert(names: NewNames): PinAlert | null {
  const s = subject(names);
  if (!s) return null;
  return { message: `New findings on ${s}`, target: targetFor(names, 'findings') };
}
