package vault

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// Argon2id parameters for key derivation.
const (
	argon2Time    = 1
	argon2Memory  = 64 * 1024 // 64 MiB
	argon2Threads = 4
	argon2KeyLen  = 32
	saltLen       = 16
	dekLen        = 32
)

// DeriveKey derives a 256-bit key from a passphrase using Argon2id.
func DeriveKey(passphrase string, salt []byte) []byte {
	return argon2.IDKey([]byte(passphrase), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
}

// GenerateSalt returns 16 random bytes for use as a KDF salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}
	return salt, nil
}

// GenerateDEK returns 32 random bytes for use as a data encryption key.
func GenerateDEK() ([]byte, error) {
	dek := make([]byte, dekLen)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("generating DEK: %w", err)
	}
	return dek, nil
}

// Encrypt encrypts plaintext with XChaCha20-Poly1305 using the given key.
func Encrypt(key, plaintext []byte) (nonce, ciphertext []byte, err error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, fmt.Errorf("creating cipher: %w", err)
	}

	nonce = make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext = aead.Seal(nil, nonce, plaintext, nil)
	return nonce, ciphertext, nil
}

// Decrypt decrypts ciphertext with XChaCha20-Poly1305.
func Decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("wrong passphrase or corrupted vault")
	}

	return plaintext, nil
}

// LockVault encrypts secrets JSON with envelope encryption.
func LockVault(secretsJSON []byte, passphrase string) (*EncryptedVault, error) {
	salt, err := GenerateSalt()
	if err != nil {
		return nil, err
	}

	dek, err := GenerateDEK()
	if err != nil {
		return nil, err
	}

	kek := DeriveKey(passphrase, salt)

	// Encrypt DEK with KEK
	dekNonce, dekCiphertext, err := Encrypt(kek, dek)
	if err != nil {
		return nil, fmt.Errorf("encrypting DEK: %w", err)
	}

	// Encrypt data with DEK
	dataNonce, dataCiphertext, err := Encrypt(dek, secretsJSON)
	if err != nil {
		return nil, fmt.Errorf("encrypting data: %w", err)
	}

	return &EncryptedVault{
		Version:   1,
		Encrypted: true,
		KDF: KDFParams{
			Algorithm: "argon2id",
			Salt:      base64.StdEncoding.EncodeToString(salt),
			Time:      argon2Time,
			Memory:    argon2Memory,
			Threads:   argon2Threads,
		},
		DEK: Blob{
			Nonce:      base64.StdEncoding.EncodeToString(dekNonce),
			Ciphertext: base64.StdEncoding.EncodeToString(dekCiphertext),
		},
		Data: Blob{
			Nonce:      base64.StdEncoding.EncodeToString(dataNonce),
			Ciphertext: base64.StdEncoding.EncodeToString(dataCiphertext),
		},
	}, nil
}

// UnlockVault decrypts an encrypted vault, returning the plaintext secrets JSON.
func UnlockVault(ev *EncryptedVault, passphrase string) ([]byte, error) {
	salt, err := base64.StdEncoding.DecodeString(ev.KDF.Salt)
	if err != nil {
		return nil, fmt.Errorf("decoding salt: %w", err)
	}

	kek := DeriveKey(passphrase, salt)

	dekNonce, err := base64.StdEncoding.DecodeString(ev.DEK.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decoding DEK nonce: %w", err)
	}
	dekCiphertext, err := base64.StdEncoding.DecodeString(ev.DEK.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decoding DEK ciphertext: %w", err)
	}

	dek, err := Decrypt(kek, dekNonce, dekCiphertext)
	if err != nil {
		return nil, err
	}

	dataNonce, err := base64.StdEncoding.DecodeString(ev.Data.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decoding data nonce: %w", err)
	}
	dataCiphertext, err := base64.StdEncoding.DecodeString(ev.Data.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decoding data ciphertext: %w", err)
	}

	return Decrypt(dek, dataNonce, dataCiphertext)
}

// ChangePassphrase re-encrypts the DEK with a new passphrase.
// The data ciphertext is unchanged.
func ChangePassphrase(ev *EncryptedVault, oldPass, newPass string) (*EncryptedVault, error) {
	// Decrypt DEK with old passphrase
	oldSalt, err := base64.StdEncoding.DecodeString(ev.KDF.Salt)
	if err != nil {
		return nil, fmt.Errorf("decoding salt: %w", err)
	}

	oldKEK := DeriveKey(oldPass, oldSalt)

	dekNonce, err := base64.StdEncoding.DecodeString(ev.DEK.Nonce)
	if err != nil {
		return nil, fmt.Errorf("decoding DEK nonce: %w", err)
	}
	dekCiphertext, err := base64.StdEncoding.DecodeString(ev.DEK.Ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decoding DEK ciphertext: %w", err)
	}

	dek, err := Decrypt(oldKEK, dekNonce, dekCiphertext)
	if err != nil {
		return nil, err
	}

	// Re-encrypt DEK with new passphrase
	newSalt, err := GenerateSalt()
	if err != nil {
		return nil, err
	}

	newKEK := DeriveKey(newPass, newSalt)

	newDEKNonce, newDEKCiphertext, err := Encrypt(newKEK, dek)
	if err != nil {
		return nil, fmt.Errorf("encrypting DEK with new passphrase: %w", err)
	}

	result := *ev
	result.KDF.Salt = base64.StdEncoding.EncodeToString(newSalt)
	result.DEK.Nonce = base64.StdEncoding.EncodeToString(newDEKNonce)
	result.DEK.Ciphertext = base64.StdEncoding.EncodeToString(newDEKCiphertext)
	// Data blob is unchanged

	return &result, nil
}

// marshalEncryptedVault serializes an EncryptedVault to JSON.
func marshalEncryptedVault(ev *EncryptedVault) ([]byte, error) {
	return json.MarshalIndent(ev, "", "  ")
}

// unmarshalEncryptedVault deserializes JSON into an EncryptedVault.
func unmarshalEncryptedVault(data []byte) (*EncryptedVault, error) {
	var ev EncryptedVault
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, fmt.Errorf("parsing encrypted vault: %w", err)
	}
	return &ev, nil
}
