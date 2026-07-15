package keyring

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	// Use a temp dir so tests don't clobber the real machine key.
	origHome := os.Getenv("HOME")
	if origHome == "" {
		origHome = os.Getenv("USERPROFILE")
	}
	tmpHome := t.TempDir()
	if os.PathSeparator == '\\' {
		os.Setenv("USERPROFILE", tmpHome)
	} else {
		os.Setenv("HOME", tmpHome)
	}
	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origHome)
	}()

	original := "sk-ant-abcdef123456"
	enc, err := Encrypt(original)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if enc == original {
		t.Error("encrypted text should differ from plaintext")
	}
	dec, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if dec != original {
		t.Errorf("Decrypt = %q, want %q", dec, original)
	}
}

func TestWrapUnwrap(t *testing.T) {
	origHome := os.Getenv("HOME")
	if origHome == "" {
		origHome = os.Getenv("USERPROFILE")
	}
	tmpHome := t.TempDir()
	if os.PathSeparator == '\\' {
		os.Setenv("USERPROFILE", tmpHome)
	} else {
		os.Setenv("HOME", tmpHome)
	}
	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origHome)
	}()

	wrapped, err := Wrap("my-api-key")
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	unwrapped, err := Unwrap(wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if unwrapped != "my-api-key" {
		t.Errorf("Unwrap = %q, want %q", unwrapped, "my-api-key")
	}
}

func TestUnwrapLegacy(t *testing.T) {
	// Legacy plaintext values must be returned as-is.
	plain, err := Unwrap("sk-ant-plaintext")
	if err != nil {
		t.Fatalf("Unwrap plaintext: %v", err)
	}
	if plain != "sk-ant-plaintext" {
		t.Errorf("Unwrap(plaintext) = %q, want %q", plain, "sk-ant-plaintext")
	}
}

func TestMachineKeyCreated(t *testing.T) {
	origHome := os.Getenv("HOME")
	if origHome == "" {
		origHome = os.Getenv("USERPROFILE")
	}
	tmpHome := t.TempDir()
	if os.PathSeparator == '\\' {
		os.Setenv("USERPROFILE", tmpHome)
	} else {
		os.Setenv("HOME", tmpHome)
	}
	defer func() {
		os.Setenv("HOME", origHome)
		os.Setenv("USERPROFILE", origHome)
	}()

	key, err := loadOrCreateKey()
	if err != nil {
		t.Fatalf("loadOrCreateKey: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("key length = %d, want 32", len(key))
	}
	// Verify the file was created.
	_, err = os.Stat(filepath.Join(tmpHome, ".boltcode", ".key"))
	if err != nil {
		t.Fatalf("machine key file not created: %v", err)
	}
}
