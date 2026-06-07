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

// Shown on the Cost KPI when any client/server has `mixed` provenance. The
// honesty contract: provenance describes which declaration priced the
// traffic, never what the upstream client actually ran.
export const MIXED_PROVENANCE_NOTE =
  'Cost is priced by your declared models, not observed client behavior. ' +
  'A blend of rates means a client hit servers priced at different models, or a declaration changed.';

// Tooltip on a `none` effective-model cell: traffic ran but nothing priced it.
export const UNPRICED_NOTE =
  'Traffic observed but no pricing model applied, so cost is $0. ' +
  'Declare a client or server model to price it.';
