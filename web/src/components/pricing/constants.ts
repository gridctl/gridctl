// Shared copy for the pricing-attribution surfaces. Every surface that
// renders a model pill or picker repeats the same precedence story so the
// mental model never shifts between the metrics tables, the sidebar
// inspector, and the pricing manager.

export const MODEL_PRECEDENCE_HINT =
  'Pricing precedence: client model (client_models) > server model > gateway default_model. ' +
  'A declared client model is a session-level default — it cannot track mid-session model switches.';

// Rendered under the Cost KPI when no attribution is configured anywhere.
// Points at the in-UI edit path first; stack.yaml stays the fallback for
// operators who prefer the file.
export const ATTRIBUTION_HINT =
  'Set a pricing model in the Top Clients table below, or in stack.yaml, to enable estimates';

// Footer copy for pickers and the pricing manager.
export const SNAPSHOT_NOTE =
  'Models come from the embedded LiteLLM snapshot (refreshed via make update-pricing at gateway rebuild).';
export const UNKNOWN_MODEL_NOTE = 'Unknown model · records tokens but prices as $0';
