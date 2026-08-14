// A stable session id for this tab, sent as X-Session-ID so the backend can
// enforce per-session concurrency caps separately from global ones. Generated
// lazily the first time a wizard request is made, so users who never open the
// wizard never spend the entropy.
//
// The probe and the OpenAPI spec preview share one id on purpose: they are
// separate endpoints with separate limiters, but both are driven by the same
// operator in the same tab, and a shared id keeps that attribution honest.
let sessionId: string | null = null;

export function getWizardSessionId(): string {
  if (sessionId) return sessionId;
  sessionId = `wizard-${Math.random().toString(36).slice(2, 10)}-${Date.now()}`;
  return sessionId;
}
