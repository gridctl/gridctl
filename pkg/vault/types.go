package vault

// Secret represents a stored secret.
type Secret struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Set   string `json:"set,omitempty"` // reserved for Phase 2: variable sets
}
