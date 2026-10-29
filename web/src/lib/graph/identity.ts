/**
 * Stable graph identities for the stack canvas.
 *
 * React Flow re-adopts a node only when the object reference is the same, and
 * it writes measured, selected, dragging, width, and height onto that object.
 * Those managed fields are not compared. A changed node copies measured,
 * selected, and dragging forward so a poll does not drop a selection or a
 * measurement. When every element is reused in the same order, the previous
 * array is returned so subscribers do not see a new reference.
 */

import type { Edge, Node } from '@xyflow/react';

const NODE_FIELDS = [
  'id',
  'type',
  'position',
  'data',
  'draggable',
  'selectable',
  'focusable',
  'ariaLabel',
  'className',
  'style',
] as const;

const EDGE_FIELDS = [
  'id',
  'source',
  'target',
  'sourceHandle',
  'targetHandle',
  'type',
  'animated',
  'data',
  'markerEnd',
  'style',
  'className',
] as const;

const CARRIED_FIELDS = ['measured', 'selected', 'dragging'] as const;

function isPlainObject(value: object): boolean {
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

/**
 * Deep equality for plain data. Arrays and plain objects compare structurally.
 * Leaves use Object.is, so NaN matches NaN and -0 does not match 0.
 */
export function structurallyEqual(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) {
    return false;
  }
  if (a instanceof Date || b instanceof Date) {
    return a instanceof Date && b instanceof Date && a.getTime() === b.getTime();
  }
  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false;
    for (let i = 0; i < a.length; i += 1) {
      if (!structurallyEqual(a[i], b[i])) return false;
    }
    return true;
  }
  if (!isPlainObject(a) || !isPlainObject(b)) return false;
  const aKeys = Object.keys(a);
  const bRecord = b as Record<string, unknown>;
  if (aKeys.length !== Object.keys(b).length) return false;
  for (const key of aKeys) {
    if (!Object.prototype.hasOwnProperty.call(b, key)) return false;
    if (!structurallyEqual((a as Record<string, unknown>)[key], bRecord[key])) return false;
  }
  return true;
}

function fieldsEqual(prev: object, next: object, fields: readonly string[]): boolean {
  const left = prev as Record<string, unknown>;
  const right = next as Record<string, unknown>;
  for (const field of fields) {
    if (!structurallyEqual(left[field], right[field])) return false;
  }
  return true;
}

function carryManaged<T extends object>(prev: T, next: T): T {
  const source = prev as Record<string, unknown>;
  const carried: Record<string, unknown> = {};
  for (const field of CARRIED_FIELDS) {
    if (source[field] !== undefined) carried[field] = source[field];
  }
  if (Object.keys(carried).length === 0) return next;
  return { ...next, ...carried };
}

function reuseById<T extends { id: string }>(
  prev: T[],
  next: T[],
  equal: (prevItem: T, nextItem: T) => boolean,
  carry: (prevItem: T, nextItem: T) => T,
): T[] {
  const prevById = new Map<string, T>();
  for (const item of prev) prevById.set(item.id, item);

  let allReused = prev.length === next.length;
  const reused = next.map((item, index) => {
    const previous = prevById.get(item.id);
    if (previous && equal(previous, item)) {
      if (allReused && prev[index] !== previous) allReused = false;
      return previous;
    }
    allReused = false;
    return previous ? carry(previous, item) : item;
  });
  return allReused ? prev : reused;
}

export function reuseGraphIdentities(
  prevNodes: Node[],
  nextNodes: Node[],
  prevEdges: Edge[],
  nextEdges: Edge[],
): { nodes: Node[]; edges: Edge[] } {
  return {
    nodes: reuseById(
      prevNodes,
      nextNodes,
      (prev, next) => fieldsEqual(prev, next, NODE_FIELDS),
      carryManaged,
    ),
    edges: reuseById(
      prevEdges,
      nextEdges,
      (prev, next) => fieldsEqual(prev, next, EDGE_FIELDS),
      carryManaged,
    ),
  };
}
