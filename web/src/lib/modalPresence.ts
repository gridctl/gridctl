// Counts open modal dialogs so background notifications can wait for them to
// close. useFocusTrap registers automatically; a dialog that traps focus
// another way registers itself through enterModal.

let openCount = 0;
let listeners: (() => void)[] = [];

function notify() {
  listeners.forEach((l) => l());
}

export function subscribeModalPresence(listener: () => void): () => void {
  listeners.push(listener);
  return () => {
    listeners = listeners.filter((l) => l !== listener);
  };
}

export function isModalOpen(): boolean {
  return openCount > 0;
}

/** Marks a modal as open and returns its release. Releasing twice is a no-op. */
export function enterModal(): () => void {
  openCount += 1;
  notify();
  let released = false;
  return () => {
    if (released) return;
    released = true;
    openCount -= 1;
    notify();
  };
}
