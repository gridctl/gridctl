import { useState } from 'react';

// useHeldValue passes `value` through until `hold` is set, then keeps the value
// captured when the hold began. A change of `epoch` re-captures the current
// value once without releasing the hold. State is adjusted during render, so
// a release or re-capture shows the new value in the same frame.
export function useHeldValue<T>(value: T, hold: boolean, epoch = 0): T {
  const [held, setHeld] = useState({ value, epoch });
  const adopt = !hold || held.epoch !== epoch;
  if (adopt && (!Object.is(held.value, value) || held.epoch !== epoch)) setHeld({ value, epoch });
  return adopt ? value : held.value;
}
