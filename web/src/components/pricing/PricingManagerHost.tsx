import { useMemo } from 'react';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { PricingManagerSlideOver } from './PricingManagerSlideOver';

// PricingManagerHost wires the canonical pricing manager to the main
// window's stores: visibility from useUIStore (so Metrics, the inspector,
// and the command palette can all open it), data and optimistic updates
// from useStackStore. The detached metrics window hosts the same slide-over
// from its own local state instead.
export function PricingManagerHost() {
  const open = useUIStore((s) => s.pricingManagerOpen);
  const setOpen = useUIStore((s) => s.setPricingManagerOpen);

  const mcpServers = useStackStore((s) => s.mcpServers);
  const defaultModel = useStackStore((s) => s.defaultModel);
  const clientModels = useStackStore((s) => s.clientModels);
  const costUsage = useStackStore((s) => s.costUsage);
  const tokenUsage = useStackStore((s) => s.tokenUsage);
  const costAttribution = useStackStore((s) => s.costAttribution);
  const setClientModelLocal = useStackStore((s) => s.setClientModelLocal);
  const setServerModelLocal = useStackStore((s) => s.setServerModelLocal);
  const setDefaultModelLocal = useStackStore((s) => s.setDefaultModelLocal);

  const servers = useMemo(
    () =>
      [...mcpServers]
        .sort((a, b) => a.name.localeCompare(b.name))
        .map((s) => ({ name: s.name, declaredModel: s.model })),
    [mcpServers],
  );

  // Declared clients plus clients observed in token/cost data, so a client
  // that has called through the gateway is configurable before anyone
  // declares it.
  const clients = useMemo(() => {
    const names = new Set<string>(Object.keys(clientModels));
    for (const name of Object.keys(tokenUsage?.per_client ?? {})) names.add(name);
    for (const name of Object.keys(costUsage?.per_client ?? {})) names.add(name);
    return [...names].sort().map((name) => ({ name, declaredModel: clientModels[name] }));
  }, [clientModels, tokenUsage, costUsage]);

  return (
    <PricingManagerSlideOver
      open={open}
      onClose={() => setOpen(false)}
      defaultModel={defaultModel}
      servers={servers}
      clients={clients}
      costAttribution={costAttribution}
      onClientSaved={setClientModelLocal}
      onServerSaved={setServerModelLocal}
      onDefaultSaved={setDefaultModelLocal}
    />
  );
}

export default PricingManagerHost;
