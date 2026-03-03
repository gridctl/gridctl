package vault

// Secret represents a stored secret.
type Secret struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Set   string `json:"set,omitempty"`
}

// Set represents a named group of secrets.
type Set struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SetSummary is a set with its member count.
type SetSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Count       int    `json:"count"`
}

// storeData is the JSON schema for secrets.json.
type storeData struct {
	Secrets []Secret `json:"secrets"`
	Sets    []Set    `json:"sets,omitempty"`
}
