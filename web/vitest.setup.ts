import { afterEach } from 'vitest';

// Polyfill ResizeObserver — used by cmdk, not implemented in jsdom
class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver = ResizeObserver;

// Polyfill scrollIntoView — used by cmdk for keyboard navigation, not in jsdom
Element.prototype.scrollIntoView = function () {};

// jsdom reports visibilityState "prerender", which would pause every poller.
// A real tab starts visible. Tests opt into hidden with defineProperty; this
// getter is the default and is restored if a test replaces the property.
let visibilityState: DocumentVisibilityState = 'visible';

function installVisibility() {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => visibilityState,
  });
}

installVisibility();

afterEach(() => {
  visibilityState = 'visible';
  installVisibility();
});
