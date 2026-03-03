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

// EncryptedVault is the JSON schema for secrets.enc (envelope-encrypted vault).
type EncryptedVault struct {
	Version   int       `json:"version"`
	Encrypted bool      `json:"encrypted"`
	KDF       KDFParams `json:"kdf"`
	DEK       Blob      `json:"dek"`
	Data      Blob      `json:"data"`
}

// KDFParams holds the Argon2id key derivation parameters.
type KDFParams struct {
	Algorithm string `json:"algorithm"`
	Salt      string `json:"salt"`   // base64
	Time      uint32 `json:"time"`   // iterations
	Memory    uint32 `json:"memory"` // KiB
	Threads   uint8  `json:"threads"`
}

// Blob holds a nonce + ciphertext pair for XChaCha20-Poly1305.
type Blob struct {
	Nonce      string `json:"nonce"`      // base64
	Ciphertext string `json:"ciphertext"` // base64
}
