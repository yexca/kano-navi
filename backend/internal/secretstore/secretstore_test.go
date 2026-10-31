package secretstore

import (
	"os"
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

// An externally supplied ciphertext of "synthetic-value" under the same
// synthetic key must decrypt, which pins the stored format.
func TestReadsTypeScriptCiphertext(t *testing.T) {
	value := os.Getenv("KANO_TS_CIPHERTEXT")
	if value == "" {
		t.Skip("no TypeScript ciphertext supplied")
	}
	plain, err := Decrypt(value, "synthetic-key")
	if err != nil || plain != "synthetic-value" {
		t.Fatalf("typescript ciphertext: %q %v", plain, err)
	}
}
