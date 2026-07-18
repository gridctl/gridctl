// Package mcpauth implements downstream OAuth 2.1 brokering for external
// MCP servers: authorization-server discovery (RFC 9728 / RFC 8414),
// dynamic client registration (RFC 7591), the authorization-code + PKCE
// flow, and encrypted token persistence with refresh rotation.
//
// The name avoids "auth" to keep it distinct from the gateway's inbound
// API authentication (internal/api/auth.go).
package mcpauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"

	"github.com/gridctl/gridctl/pkg/state"
	"github.com/gridctl/gridctl/pkg/vault"
)

// lockName is the cross-process flock name used for all store mutations.
const lockName = "oauth"

// lockTimeout bounds how long a store operation waits on the flock.
const lockTimeout = 5 * time.Second

// Grant is one authorization against a downstream server, keyed by the
// canonical resource URL. Two stacks pointing at the same endpoint share a
// grant, and renaming a server in stack.yaml does not force a re-login.
type Grant struct {
	Resource  string        `json:"resource"`         // canonical resource URL
	Issuer    string        `json:"issuer"`           // authorization server issuer
	Scopes    []string      `json:"scopes,omitempty"` // granted scopes
	Token     *oauth2.Token `json:"token"`            // access + refresh token
	UpdatedAt time.Time     `json:"updatedAt"`

	// Endpoints captured at authorization time so refresh and revocation
	// work across daemon restarts without re-running discovery.
	TokenEndpoint      string `json:"tokenEndpoint"`
	RevocationEndpoint string `json:"revocationEndpoint,omitempty"`
}

// ClientRegistration is a dynamically registered OAuth client, keyed by
// issuer (SEP-2352: credentials are bound to the issuing AS and never
// reused with a different one).
type ClientRegistration struct {
	Issuer       string    `json:"issuer"`
	ClientID     string    `json:"clientId"`
	ClientSecret string    `json:"clientSecret,omitempty"`
	RedirectURI  string    `json:"redirectUri"`
	CreatedAt    time.Time `json:"createdAt"`
}

// storeData is the plaintext (pre-encryption) shape of the store file.
type storeData struct {
	Version       int                           `json:"version"`
	Grants        map[string]Grant              `json:"grants"`        // canonical resource URL -> grant
	Registrations map[string]ClientRegistration `json:"registrations"` // issuer -> registered client
}

// encryptedStore is the on-disk shape: a single XChaCha20-Poly1305 blob
// sealed with the machine-local key (no passphrase involved, so the daemon
// can read it across restarts without any unlock step).
type encryptedStore struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// TokenStore persists OAuth grants and client registrations encrypted at
// rest under the gridctl state directory. All operations take a cross-
// process flock and re-read the file, so a CLI-driven authorization is
// observed by the running daemon on its next access.
type TokenStore struct {
	dir string
	key []byte // 32-byte machine-local key
}

// NewTokenStore opens (creating if needed) the token store rooted at dir.
// When dir is empty, <state base dir>/oauth is used.
func NewTokenStore(dir string) (*TokenStore, error) {
	if dir == "" {
		dir = filepath.Join(state.BaseDir(), "oauth")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating oauth state dir: %w", err)
	}
	key, err := loadOrCreateMachineKey(dir)
	if err != nil {
		return nil, fmt.Errorf("loading machine key: %w", err)
	}
	return &TokenStore{dir: dir, key: key}, nil
}

func (s *TokenStore) path() string {
	return filepath.Join(s.dir, "tokens.enc")
}

// load reads and decrypts the store file. A missing file yields an empty
// store; a corrupt file yields an error the caller degrades to needs-auth
// (never a crash).
func (s *TokenStore) load() (*storeData, error) {
	raw, err := os.ReadFile(s.path())
	if os.IsNotExist(err) {
		return &storeData{Version: 1, Grants: map[string]Grant{}, Registrations: map[string]ClientRegistration{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading token store: %w", err)
	}

	var enc encryptedStore
	if err := json.Unmarshal(raw, &enc); err != nil {
		return nil, fmt.Errorf("parsing token store: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(enc.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decoding token store nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(enc.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decoding token store ciphertext: %w", err)
	}
	plain, err := vault.Decrypt(s.key, nonce, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypting token store: %w", err)
	}

	var data storeData
	if err := json.Unmarshal(plain, &data); err != nil {
		return nil, fmt.Errorf("parsing token store contents: %w", err)
	}
	if data.Grants == nil {
		data.Grants = map[string]Grant{}
	}
	if data.Registrations == nil {
		data.Registrations = map[string]ClientRegistration{}
	}
	return &data, nil
}

func (s *TokenStore) save(data *storeData) error {
	plain, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshaling token store: %w", err)
	}
	nonce, ciphertext, err := vault.Encrypt(s.key, plain)
	if err != nil {
		return fmt.Errorf("encrypting token store: %w", err)
	}
	enc := encryptedStore{
		Version:    1,
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	out, err := json.Marshal(enc)
	if err != nil {
		return fmt.Errorf("marshaling encrypted store: %w", err)
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return fmt.Errorf("writing token store: %w", err)
	}
	return os.Rename(tmp, s.path())
}

// Grant returns the stored grant for a canonical resource URL.
func (s *TokenStore) Grant(resource string) (Grant, bool, error) {
	var g Grant
	var ok bool
	err := state.WithLock(lockName, lockTimeout, func() error {
		data, err := s.load()
		if err != nil {
			return err
		}
		g, ok = data.Grants[resource]
		return nil
	})
	return g, ok, err
}

// PutGrant stores (or replaces) the grant for its resource. Called on
// initial authorization and again on every refresh-token rotation.
func (s *TokenStore) PutGrant(g Grant) error {
	g.UpdatedAt = time.Now()
	return state.WithLock(lockName, lockTimeout, func() error {
		data, err := s.load()
		if err != nil {
			return err
		}
		data.Grants[g.Resource] = g
		return s.save(data)
	})
}

// DeleteGrant removes the grant for a resource. Missing grants are a no-op.
func (s *TokenStore) DeleteGrant(resource string) error {
	return state.WithLock(lockName, lockTimeout, func() error {
		data, err := s.load()
		if err != nil {
			return err
		}
		delete(data.Grants, resource)
		return s.save(data)
	})
}

// Registration returns the stored client registration for an issuer.
func (s *TokenStore) Registration(issuer string) (ClientRegistration, bool, error) {
	var r ClientRegistration
	var ok bool
	err := state.WithLock(lockName, lockTimeout, func() error {
		data, err := s.load()
		if err != nil {
			return err
		}
		r, ok = data.Registrations[issuer]
		return nil
	})
	return r, ok, err
}

// PutRegistration stores a registered client keyed by issuer.
func (s *TokenStore) PutRegistration(r ClientRegistration) error {
	r.CreatedAt = time.Now()
	return state.WithLock(lockName, lockTimeout, func() error {
		data, err := s.load()
		if err != nil {
			return err
		}
		data.Registrations[r.Issuer] = r
		return s.save(data)
	})
}

// DeleteRegistration removes the registered client for an issuer.
func (s *TokenStore) DeleteRegistration(issuer string) error {
	return state.WithLock(lockName, lockTimeout, func() error {
		data, err := s.load()
		if err != nil {
			return err
		}
		delete(data.Registrations, issuer)
		return s.save(data)
	})
}

// GrantsByIssuerResource reports how many stored grants share the given
// issuer and resource; used for the cross-stack refcount before revoking
// on server removal.
func (s *TokenStore) Grants() (map[string]Grant, error) {
	var out map[string]Grant
	err := state.WithLock(lockName, lockTimeout, func() error {
		data, err := s.load()
		if err != nil {
			return err
		}
		out = data.Grants
		return nil
	})
	return out, err
}
