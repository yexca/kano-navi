package secretstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRoundTripAndFailures(t *testing.T) {
	sealed, err := Encrypt("synthetic-value", "synthetic-key")
	if err != nil || sealed == "" {
		t.Fatalf("encrypt: %v", err)
	}
	plain, err := Decrypt(sealed, "synthetic-key")
	if err != nil || plain != "synthetic-value" {
		t.Fatalf("decrypt: %q %v", plain, err)
	}
	if _, err = Decrypt(sealed, "another-key"); err != ErrDecrypt {
		t.Fatalf("wrong key: %v", err)
	}
	if _, err = Decrypt("v0:a:b:c", "synthetic-key"); err != ErrFormat {
		t.Fatalf("format: %v", err)
	}
	if _, err = Encrypt("value", ""); err != ErrKeyRequired {
		t.Fatalf("missing key: %v", err)
	}
	if sealed, err = Encrypt("", "synthetic-key"); err != nil || sealed != "" {
		t.Fatalf("empty: %q %v", sealed, err)
	}
}

// This fixture is produced by the TypeScript server/secret-store.ts format
// with Node crypto, a fixed nonce, and the same synthetic key. Keeping it in
// the repository makes the compatibility check mandatory in every test run.
func TestReadsTypeScriptCiphertext(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "typescript-ciphertext.txt"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := Decrypt(strings.TrimSpace(string(fixture)), "synthetic-key")
	if err != nil || plain != "synthetic-value" {
		t.Fatalf("typescript ciphertext: %q %v", plain, err)
	}
}
