import { useEffect, useState } from 'react';
import { fetchPricingModels } from '../lib/api';

// Window-scoped cache for the known-models list. The embedded pricing
// snapshot only changes on a gateway rebuild, so one fetch serves every
// picker in this window (main or detached). The in-flight promise is shared
// so simultaneous first-renders coalesce into a single request.
let cachedModels: string[] | null = null;
let inFlight: Promise<string[]> | null = null;

function loadPricingModels(): Promise<string[]> {
  if (cachedModels !== null) return Promise.resolve(cachedModels);
  if (inFlight) return inFlight;
  inFlight = fetchPricingModels()
    .then((resp) => {
      cachedModels = resp.models;
      return resp.models;
    })
    .finally(() => {
      inFlight = null;
    });
  return inFlight;
}

// usePricingModels returns the canonical model IDs known to the active
// pricing source, fetching them once per window. Returns an empty array
// until loaded (or on fetch failure — pickers degrade to free text; pricing
// is best-effort anyway).
export function usePricingModels(): string[] {
  const [models, setModels] = useState<string[]>(cachedModels ?? []);

  useEffect(() => {
    if (cachedModels !== null) return;
    let cancelled = false;
    loadPricingModels()
      .then((list) => {
        if (!cancelled) setModels(list);
      })
      .catch(() => {
        // Degrade to free text.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return models;
}

// resetPricingModelsCacheForTests clears the module cache so unit tests can
// exercise the fetch path in isolation.
export function resetPricingModelsCacheForTests(): void {
  cachedModels = null;
  inFlight = null;
}
