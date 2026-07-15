// Package keyring provides encryption-at-rest for API keys stored in
// ~/.boltcode/agent.json. It generates a per-machine key on first use and
// encrypts/decrypts using AES-256-GCM so credentials are never stored as
// plaintext on disk.
package keyring

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const keySize = 32 // AES-256

// machineKeyPath returns the path to the machine-local encryption key.
func machineKeyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".boltcode", ".key")
}

// encrypt encrypts plaintext using AES-256-GCM with the machine key.
// Output is base64-encoded: nonce (12 bytes) || ciphertext.
func encrypt(plaintext []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return base64.RawStdEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

// decrypt decrypts a base64-encoded payload with AES-256-GCM.
func decrypt(encoded string, key []byte) ([]byte, error) {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid encoding: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong key or corrupt data): %w", err)
	}
	return plaintext, nil
}

// loadOrCreateKey loads the machine key from disk, generating one if missing.
// The key is stored at ~/.boltcode/.key with 0600 permissions.
func loadOrCreateKey() ([]byte, error) {
	path := machineKeyPath()
	data, err := os.ReadFile(path)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	// Generate a new key.
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating machine key: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// Encrypt encrypts a string value (e.g. an API key) for storage.
func Encrypt(plaintext string) (string, error) {
	key, err := loadOrCreateKey()
	if err != nil {
		return "", err
	}
	return encrypt([]byte(plaintext), key)
}

// Decrypt decrypts a previously encrypted string value.
func Decrypt(encoded string) (string, error) {
	key, err := loadOrCreateKey()
	if err != nil {
		return "", err
	}
	data, err := decrypt(encoded, key)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// WrappedSecret holds an encrypted value with a marker so the system can
// distinguish encrypted secrets from plaintext (e.g. during migration).
type WrappedSecret struct {
	Encrypted string `json:"encrypted"`
}

// Wrap encrypts the value and returns a JSON-wrapped marker.
func Wrap(plaintext string) (string, error) {
	enc, err := Encrypt(plaintext)
	if err != nil {
		return "", err
	}
	w := WrappedSecret{Encrypted: enc}
	data, _ := json.Marshal(w)
	return string(data), nil
}

// Unwrap decrypts a value that was wrapped with Wrap. If the input is not JSON
// (legacy plaintext), it is returned as-is for backward compatibility.
func Unwrap(wrapped string) (string, error) {
	var w WrappedSecret
	if err := json.Unmarshal([]byte(wrapped), &w); err != nil {
		// Not wrapped — return as plaintext (legacy data).
		return wrapped, nil
	}
	return Decrypt(w.Encrypted)
}
