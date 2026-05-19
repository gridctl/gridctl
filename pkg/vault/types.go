package vault

// VariableType enumerates the supported value types for stored variables.
// PR 1 records the type as metadata only; expansion treats every value as a
// string. PR 2 will rewrite ${var:KEY} expansion to honour the type.
type VariableType string

const (
	TypeString VariableType = "string"
	TypeJSON   VariableType = "json"
	TypeList   VariableType = "list"
	TypeNumber VariableType = "number"
	TypeBool   VariableType = "bool"
)

// Variable represents a stored variable. Secrets and non-sensitive
// configuration share the same struct; IsSecret gates redaction and
// the on-disk encryption envelope is unchanged either way.
type Variable struct {
	Key      string       `json:"key"`
	Value    string       `json:"value"`
	Type     VariableType `json:"type"`
	IsSecret bool         `json:"is_secret"`
	Set      string       `json:"set,omitempty"`
}

// Set represents a named group of variables.
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

// CurrentStoreVersion is the on-disk schema version this build emits.
// v0 (legacy flat array) and v1 (object with "secrets" key) loads are
// supported but every save rewrites the file as v2.
const CurrentStoreVersion = 2

// storeData is the v2 on-disk schema for secrets.json. The plaintext bytes
// inside an encrypted vault share this shape.
type storeData struct {
	Version   int        `json:"version"`
	Variables []Variable `json:"variables"`
	Sets      []Set      `json:"sets,omitempty"`
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

// IsValidType reports whether t is one of the supported variable types.
func IsValidType(t VariableType) bool {
	switch t {
	case TypeString, TypeJSON, TypeList, TypeNumber, TypeBool:
		return true
	}
	return false
}
