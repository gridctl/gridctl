import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react';

interface UseEscapeLayerOptions {
  /** Whether the layer is open and holds a place in the stack. */
  active: boolean;
  /** Called when Escape reaches this layer while it is on top. */
  onEscape: () => void;
  /** Modal layers outrank every non-modal layer. */
  modal: boolean;
  /** The layer's root element, used to rank a nested layer above its host. */
  ref?: RefObject<HTMLElement | null>;
  /** Close even when the target is a field. By default Escape in an input,
   *  textarea, or select blurs the field and leaves the layer open. */
  closeFromFields?: boolean;
}

interface Layer {
  modal: boolean;
  ref?: RefObject<HTMLElement | null>;
  onEscape: RefObject<() => void>;
  closeFromFields: boolean;
  order: number;
}

const layers: Layer[] = [];
let nextOrder = 0;

function isAbove(a: Layer, b: Layer): boolean {
  if (a.modal !== b.modal) return a.modal;
  const ae = a.ref?.current;
  const be = b.ref?.current;
  // React runs a child's effects before its parent's, so a nested layer that
  // opens in the same commit as its host registers first. Containment ranks it.
  if (ae && be && ae !== be) {
    if (be.contains(ae)) return true;
    if (ae.contains(be)) return false;
  }
  return a.order > b.order;
}

function topLayer(): Layer | undefined {
  let top: Layer | undefined;
  for (const layer of layers) {
    if (!top || isAbove(layer, top)) top = layer;
  }
  return top;
}

// One listener for every layer. In a browser, React can commit a discrete close
// (and its effect cleanup) in the microtask between two listeners on the same
// node, so per-layer listeners would let the layer beneath see an emptied top
// and close too. The top is resolved once, before anything is invoked.
function handleKeyDown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return;
  const top = topLayer();
  if (!top) return;
  e.stopPropagation();
  const target = e.target as HTMLElement | null;
  const tag = target?.tagName;
  if (!top.closeFromFields && (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT')) {
    target?.blur();
    return;
  }
  top.onEscape.current();
}

function register(layer: Layer) {
  if (layers.length === 0) document.addEventListener('keydown', handleKeyDown);
  layers.push(layer);
}

function unregister(layer: Layer) {
  const i = layers.indexOf(layer);
  if (i === -1) return;
  layers.splice(i, 1);
  if (layers.length === 0) document.removeEventListener('keydown', handleKeyDown);
}

/**
 * Enrolls an overlay in the shared Escape stack. While several layers are
 * open, one Escape reaches only the topmost one and stops there, so the layers
 * beneath it and the shell's window-level handlers never see it.
 *
 * The listener stays in the bubble phase on purpose: ConfirmDialog and
 * useDismiss claim Escape in the capture phase on document and must keep
 * precedence over the layer that hosts them.
 */
export function useEscapeLayer({
  active,
  onEscape,
  modal,
  ref,
  closeFromFields = false,
}: UseEscapeLayerOptions): void {
  // Layout, not passive: the shared listener can run between commit and the
  // passive flush and must not call a stale close callback.
  const onEscapeRef = useRef(onEscape);
  useLayoutEffect(() => {
    onEscapeRef.current = onEscape;
  }, [onEscape]);

  useEffect(() => {
    if (!active) return;
    const layer: Layer = { modal, ref, onEscape: onEscapeRef, closeFromFields, order: nextOrder++ };
    register(layer);
    return () => unregister(layer);
  }, [active, modal, ref, closeFromFields]);
}
