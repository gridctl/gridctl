package vault

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		salt       []byte
	}{
		{"basic", "mypassword", []byte("1234567890123456")},
		{"empty passphrase", "", []byte("1234567890123456")},
		{"unicode passphrase", "p\u00e4ssw\u00f6rd", []byte("1234567890123456")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := DeriveKey(tt.passphrase, tt.salt)
			if len(key) != 32 {
				t.Errorf("key length = %d, want 32", len(key))
			}

			// Same inputs produce same key
			key2 := DeriveKey(tt.passphrase, tt.salt)
			if !bytes.Equal(key, key2) {
				t.Error("same inputs should produce same key")
			}
		})
	}
}

func TestDeriveKey_DifferentSalt(t *testing.T) {
	pass := "mypassword"
	salt1 := []byte("1234567890123456")
	salt2 := []byte("6543210987654321")

	key1 := DeriveKey(pass, salt1)
	key2 := DeriveKey(pass, salt2)

	if bytes.Equal(key1, key2) {
		t.Error("different salts should produce different keys")
	}
}

func TestDeriveKey_DifferentPassphrase(t *testing.T) {
	salt := []byte("1234567890123456")
	key1 := DeriveKey("password1", salt)
	key2 := DeriveKey("password2", salt)

	if bytes.Equal(key1, key2) {
		t.Error("different passphrases should produce different keys")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{"small", []byte("hello world")},
		{"empty", []byte("")},
		{"large", bytes.Repeat([]byte("x"), 10000)},
		{"json", []byte(`{"secrets":[{"key":"A","value":"B"}]}`)},
	}

	key := DeriveKey("testpass", []byte("1234567890123456"))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nonce, ciphertext, err := Encrypt(key, tt.plaintext)
			if err != nil {
				t.Fatalf("Encrypt error: %v", err)
			}

			if len(nonce) != 24 { // XChaCha20-Poly1305 nonce size
				t.Errorf("nonce length = %d, want 24", len(nonce))
			}

			got, err := Decrypt(key, nonce, ciphertext)
			if err != nil {
				t.Fatalf("Decrypt error: %v", err)
			}

			if !bytes.Equal(got, tt.plaintext) {
				t.Errorf("decrypted = %q, want %q", got, tt.plaintext)
			}
		})
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1 := DeriveKey("correct", []byte("1234567890123456"))
	key2 := DeriveKey("wrong", []byte("1234567890123456"))

	nonce, ciphertext, err := Encrypt(key1, []byte("secret data"))
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}

	_, err = Decrypt(key2, nonce, ciphertext)
	if err == nil {
		t.Error("expected error with wrong key")
	}
}

func TestLockUnlockVault(t *testing.T) {
	secretsJSON := []byte(`{"secrets":[{"key":"API_KEY","value":"sk-123"}],"sets":[]}`)

	ev, err := LockVault(secretsJSON, "mypassphrase")
	if err != nil {
		t.Fatalf("LockVault error: %v", err)
	}

	if !ev.Encrypted {
		t.Error("encrypted should be true")
	}
	if ev.Version != 1 {
		t.Errorf("version = %d, want 1", ev.Version)
	}
	if ev.KDF.Algorithm != "argon2id" {
		t.Errorf("algorithm = %q, want argon2id", ev.KDF.Algorithm)
	}

	got, err := UnlockVault(ev, "mypassphrase")
	if err != nil {
		t.Fatalf("UnlockVault error: %v", err)
	}

	if !bytes.Equal(got, secretsJSON) {
		t.Errorf("decrypted = %q, want %q", got, secretsJSON)
	}
}

func TestUnlockVault_WrongPassphrase(t *testing.T) {
	secretsJSON := []byte(`{"key":"value"}`)

	ev, err := LockVault(secretsJSON, "correct")
	if err != nil {
		t.Fatalf("LockVault error: %v", err)
	}

	_, err = UnlockVault(ev, "wrong")
	if err == nil {
		t.Error("expected error with wrong passphrase")
	}
}

func TestChangePassphrase(t *testing.T) {
	secretsJSON := []byte(`{"secrets":[{"key":"DB_PASS","value":"s3cret"}]}`)

	ev, err := LockVault(secretsJSON, "oldpass")
	if err != nil {
		t.Fatalf("LockVault error: %v", err)
	}

	changed, err := ChangePassphrase(ev, "oldpass", "newpass")
	if err != nil {
		t.Fatalf("ChangePassphrase error: %v", err)
	}

	// Unlock with new passphrase should work
	got, err := UnlockVault(changed, "newpass")
	if err != nil {
		t.Fatalf("UnlockVault with new passphrase error: %v", err)
	}
	if !bytes.Equal(got, secretsJSON) {
		t.Errorf("decrypted = %q, want %q", got, secretsJSON)
	}

	// Unlock with old passphrase should fail
	_, err = UnlockVault(changed, "oldpass")
	if err == nil {
		t.Error("expected error with old passphrase after change")
	}

	// Data blob should be unchanged (only DEK wrapper changes)
	if ev.Data.Nonce != changed.Data.Nonce {
		t.Error("data nonce should be unchanged after passphrase change")
	}
	if ev.Data.Ciphertext != changed.Data.Ciphertext {
		t.Error("data ciphertext should be unchanged after passphrase change")
	}
}

func TestChangePassphrase_WrongOldPassphrase(t *testing.T) {
	secretsJSON := []byte(`{"key":"value"}`)

	ev, err := LockVault(secretsJSON, "correct")
	if err != nil {
		t.Fatalf("LockVault error: %v", err)
	}

	_, err = ChangePassphrase(ev, "wrong", "newpass")
	if err == nil {
		t.Error("expected error with wrong old passphrase")
	}
}

func TestEncryptedVault_JSON(t *testing.T) {
	secretsJSON := []byte(`{"secrets":[]}`)

	ev, err := LockVault(secretsJSON, "test")
	if err != nil {
		t.Fatalf("LockVault error: %v", err)
	}

	data, err := marshalEncryptedVault(ev)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	ev2, err := unmarshalEncryptedVault(data)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Verify round-trip preserves fields
	if ev.Version != ev2.Version {
		t.Errorf("version mismatch: %d vs %d", ev.Version, ev2.Version)
	}
	if ev.KDF.Salt != ev2.KDF.Salt {
		t.Error("salt mismatch after JSON round-trip")
	}
	if ev.DEK.Ciphertext != ev2.DEK.Ciphertext {
		t.Error("DEK ciphertext mismatch after JSON round-trip")
	}
	if ev.Data.Ciphertext != ev2.Data.Ciphertext {
		t.Error("data ciphertext mismatch after JSON round-trip")
	}

	// Verify the round-tripped vault can still be unlocked
	got, err := UnlockVault(ev2, "test")
	if err != nil {
		t.Fatalf("UnlockVault after round-trip error: %v", err)
	}
	if !bytes.Equal(got, secretsJSON) {
		t.Errorf("decrypted = %q, want %q", got, secretsJSON)
	}
}

func TestEncryptedVault_JSONFormat(t *testing.T) {
	secretsJSON := []byte(`{"secrets":[]}`)

	ev, err := LockVault(secretsJSON, "test")
	if err != nil {
		t.Fatalf("LockVault error: %v", err)
	}

	data, err := marshalEncryptedVault(ev)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Verify the JSON has expected top-level fields
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	for _, field := range []string{"version", "encrypted", "kdf", "dek", "data"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("missing field %q in JSON", field)
		}
	}
}

func TestGenerateSalt_Uniqueness(t *testing.T) {
	salt1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt error: %v", err)
	}

	salt2, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt error: %v", err)
	}

	if bytes.Equal(salt1, salt2) {
		t.Error("two GenerateSalt calls should produce different values")
	}

	if len(salt1) != 16 {
		t.Errorf("salt length = %d, want 16", len(salt1))
	}
}

func TestGenerateDEK_Length(t *testing.T) {
	dek, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK error: %v", err)
	}

	if len(dek) != 32 {
		t.Errorf("DEK length = %d, want 32", len(dek))
	}
}

func TestGenerateDEK_Uniqueness(t *testing.T) {
	dek1, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK error: %v", err)
	}

	dek2, err := GenerateDEK()
	if err != nil {
		t.Fatalf("GenerateDEK error: %v", err)
	}

	if bytes.Equal(dek1, dek2) {
		t.Error("two GenerateDEK calls should produce different values")
	}
}
